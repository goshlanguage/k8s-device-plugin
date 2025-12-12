package plugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"google.golang.org/grpc"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	status "google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	resourceDomain = "tenstorrent.com"
	socketName     = "tenstorrent.sock"
)

// DevicePlugin should conform to the DevicePluginServer Interface as seen here:
//     https://github.com/kubernetes/kubelet/blob/v0.34.3/pkg/apis/deviceplugin/v1beta1/api_grpc.pb.go#L264
//
// Conceptual documentation for device plugins can be found on the kubernetes docs:
// 		 https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-implementation
//
// Lastly, the original design doc can be of benefit when conceptualizing the operational flow of a device plugin:
//     https://github.com/kubernetes/design-proposals-archive/blob/main/resource-management/device-plugin.md
type DevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer

	ctx     context.Context
	devices []*pluginapi.Device
	socket  string
}

// NewDevicePlugin should enumerate a hosts' tenstorrent devices
// TODO: Remove this stub
func NewDevicePlugin() *DevicePlugin {
	return &DevicePlugin{
		ctx: context.Background(),
		devices: []*pluginapi.Device{
			{ID: "0", Health: pluginapi.Healthy},
			{ID: "1", Health: pluginapi.Healthy},
			{ID: "2", Health: pluginapi.Healthy},
			{ID: "3", Health: pluginapi.Healthy},
		},
		socket: path.Join(pluginapi.DevicePluginPath, socketName),
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
		klog.Info("ListAndWatch: sending device list")
		if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: dp.devices}); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
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
	devs := []*pluginapi.DeviceSpec{
		{
			HostPath:      "/dev/tenstorrent",
			ContainerPath: "/dev/tenstorrent",
			Permissions:   "rw",
		},
	}

	resp := &pluginapi.AllocateResponse{
		ContainerResponses: []*pluginapi.ContainerAllocateResponse{
			{
				Envs: map[string]string{
					"TT_VISIBLE_DEVICES": req.ContainerRequests[0].DevicesIds[0],
				},
				Devices: devs,
			},
		},
	}

	return resp, nil
}

// PreStartContainer is called, if indicated by Device Plugin during registration phase,
// before each container start. Device plugin can run device specific operations
// such as resetting the device before making devices available to the container.
func (dp *DevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// Start initiates the gRPC server for the device plugin
func (dp *DevicePlugin) Start() error {
	// Remove if exists
	os.Remove(dp.socket)

	// Start gRPC server
	sock, err := net.Listen("unix", dp.socket)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %v", err)
	}

	klog.Infof("gRPC socket established at %s", dp.socket)

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, dp)

	go grpcServer.Serve(sock)

	return dp.Register(pluginapi.KubeletSocket)
}

func (dp *DevicePlugin) Register(kubeletEndpoint string) error {
	conn, err := dp.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)

	req := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     kubeletEndpoint,
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
func (dp *DevicePlugin) dial() (*grpc.ClientConn, error) {
	connectParams := grpc.ConnectParams{
		MinConnectTimeout: 5 * time.Second,
	}

	kubeletSocketEndpoint := fmt.Sprintf("unix://%s", pluginapi.KubeletSocket)

	conn, err := grpc.NewClient(
		kubeletSocketEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(connectParams),
	)
	if err != nil {
		return nil, err
	}

	klog.Infof("grpc connection created with endpoint %s", kubeletSocketEndpoint)
	klog.Infof("grpc state %s", conn.GetState().String())

	return conn, nil
}
