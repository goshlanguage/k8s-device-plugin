package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	status "google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	grpcServerStartTimeout = 10 * time.Second
	resourceDomain = "tenstorrent.com"
	socketName     = "tenstorrent.sock"
)

// DevicePlugin should conform to the DevicePluginServer Interface as seen here:
//
//	https://github.com/kubernetes/kubelet/blob/v0.34.3/pkg/apis/deviceplugin/v1beta1/api_grpc.pb.go#L264
//
// Conceptual documentation for device plugins can be found on the kubernetes docs:
//
//	https://kubernetes.kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-implementation
//
// Lastly, the original design doc can be of benefit when conceptualizing the operational flow of a device plugin:
//	https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/device-plugin.md
type DevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer

	ctx     context.Context
	// devices represent the currently discovered tenstorrent devices
	devices []*pluginapi.Device
	// resourceName represents the card(s) discovered, eg: n150 or n300
	resourceName string
	// socket represents the device plugin socket the kubelet will communicate with
	socket  string
	// socketDir is the directory where sockets are created (default: /var/lib/kubelet/device-plugins)
	socketDir  string
}

// NewDevicePlugin should enumerate a hosts' tenstorrent devices
func NewDevicePlugin(resourceName string, devices []*pluginapi.Device) *DevicePlugin {
	return &DevicePlugin{
		ctx: context.Background(),
		devices: devices,
		socket: socketName,
		socketDir: pluginapi.DevicePluginPath,
	}
}

// GetDevicePluginOptions returns options to be communicated with Device Manager.
// TODO: Implement
func (dp *DevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// ListAndWatch returns a stream of List of Devices
// Whenever a Device state change or a Device disappears, ListAndWatch
// returns the new list
func (dp *DevicePlugin) ListAndWatch(e *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	for {
		klog.Info("ListAndWatch: sending initial device list")
		if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: dp.devices}); err != nil {
			return err
		}

		// TODO: ideally we would use a channel (dp.updateCh) here 
		// to trigger sends only when hardware health changes.
		// For now, we just block to keep the stream open.
		<-dp.ctx.Done()
		return nil
	}
}

// GetPreferredAllocation returns a preferred set of devices to allocate
// from a list of available ones. The resulting preferred allocation is not
// guaranteed to be the allocation ultimately performed by the
// devicemanager. It is only designed to help the devicemanager make a more
// informed allocation decision when possible.
func (dp *DevicePlugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetPreferredAllocation not implemented")
}

// Allocate is called during container creation so that the Device
// Plugin can run device specific operations and instruct Kubelet
// of the steps to make the Device available in the container
func (dp *DevicePlugin) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	klog.Infof("Received Allocate request for %v", req.ContainerRequests)	

	response := pluginapi.AllocateResponse{}

	for _, req := range req.ContainerRequests {
		var devices []*pluginapi.DeviceSpec
		
		for _, id := range req.DevicesIds {		
			devPath := fmt.Sprintf("/dev/tenstorrent/%s", id)

			klog.Infof("Allocating: %s", devPath)

			devices = append(devices, &pluginapi.DeviceSpec{
				HostPath:      devPath,
				ContainerPath: devPath,
				Permissions:   "rw",
			})
		}

		response.ContainerResponses = append(response.ContainerResponses, &pluginapi.ContainerAllocateResponse{
			Devices: devices,
		})
	}

	return &response, nil
}

// PreStartContainer is called, if indicated by Device Plugin during registration phase,
// before each container start. Device plugin can run device specific operations
// such as resetting the device before making devices available to the container.
func (dp *DevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// Start initiates the gRPC server for the device plugin
func (dp *DevicePlugin) Start() error {
	fullSocketPath := filepath.Join(dp.socketDir, dp.socket)

	// Clean up
  os.Remove(fullSocketPath)

	// Start gRPC server
	sock, err := net.Listen("unix", fullSocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %v", err)
	}

	klog.Infof("gRPC server socket established at %s", fullSocketPath)

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, dp)

	go func() {
		if err := grpcServer.Serve(sock); err != nil {
			klog.Fatalf("gRPC Serve failed: %v", err)
		}
	}()

	// BLOCK until the server is actually reachable
	if err := dp.waitForServer(fullSocketPath, grpcServerStartTimeout); err != nil {
			return fmt.Errorf("gRPC server failed to start: %v", err)
	}

	return dp.Register(pluginapi.KubeletSocket)
}

func (dp *DevicePlugin) Register(kubeletEndpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()

	conn, err := dp.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)

	req := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     dp.socket,
		ResourceName: fmt.Sprintf("%s/n150", resourceDomain),
	}

	klog.Infof("Registering with kubelet on endpoint %s", req.Endpoint)
	klog.Infof("Registering resource %s", req.ResourceName)
	klog.Infof("Registering with device plugin API version %s", req.Version)

	_, err = client.Register(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to register with kubelet: %v", err)
	}

	return nil
}

// dial is a helper function that establishes gRPC communication with the kubelet
func (dp *DevicePlugin) dial(ctx context.Context) (*grpc.ClientConn, error) {
	kubeletSocketEndpoint := fmt.Sprintf("unix://%s", pluginapi.KubeletSocket)
	
	klog.Infof("Dialing kubelet socket: %s", kubeletSocketEndpoint)

	conn, err := grpc.NewClient(
		kubeletSocketEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	// Attempt to connect
	conn.Connect()

	// Explicitly block until READY or context deadline
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}

		if !conn.WaitForStateChange(ctx, state) {
			return nil, fmt.Errorf("gRPC connection timeout, last state: %s", state)
		}
	}

	klog.Infof("grpc connection created with endpoint %s", kubeletSocketEndpoint)
	klog.Infof("grpc state %s", conn.GetState().String())

	return conn, nil
}

// waitForServer blocks until the gRPC server is ready to accept connections
func (dp *DevicePlugin) waitForServer(socketPath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	defer conn.Close()

	conn.Connect()
	
	// We connected successfully, close this test connection
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}

		// WaitForStateChange blocks until the state changes from 'state'
		// Returns false if the context expires
		if !conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}

	return nil
}
