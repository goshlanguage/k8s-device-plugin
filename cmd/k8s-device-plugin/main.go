package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goshlanguage/k8s-device-plugin/internal/plugin"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func main() {
	discoverAndStartPlugins()
	select {} // block forever
}

// discoverAndStartPlugins iterates through the devie nodes to discover installed cards
//     the driver is expected to expose tt_card_type for discovering the resource name
func discoverAndStartPlugins() {
	devices := make(map[string][]*pluginapi.Device)

	files, err := filepath.Glob("/dev/tenstorrent/*")
	if err != nil {
		klog.Fatalf("Could not glob /dev/tenstorrent: %v", err)
	}

	for _, file := range files {
		deviceID := filepath.Base(file)
		cardTypePath := fmt.Sprintf("/sys/class/tenstorrent/tenstorrent%s/tt_card_type", deviceID)

		file, err := os.Open(cardTypePath)
		if err !=nil {
			klog.Errorf("Failed to read card information from: %s. %s", cardTypePath, err.Error())
		}

		cardType, err := io.ReadAll(file)
		if err != nil {
			klog.Warningf("Could not read card type from %s: %v", cardTypePath, err)
			continue
		}
		resourceName := strings.TrimSpace(string(cardType))
		devices[resourceName] = append(devices[resourceName], &pluginapi.Device{
			ID:     deviceID,
			Health: pluginapi.Healthy,
		})
	}

	klog.Infof("Discovered %v devices: %v", len(devices), devices)

	for resourceName, devs := range devices {
		dp := plugin.NewDevicePlugin(resourceName, devs)
		
		go func(dp *plugin.DevicePlugin) {
			klog.Infof("Starting device plugin for resource %s", resourceName)

			if err := dp.Start(); err != nil {
				klog.Fatalf("Error starting device plugin: %v", err)
			}
		}(dp)
	}
}
