package main

import (
	"github.com/goshlanguage/k8s-device-plugin/internal/plugin"
)

func main() {
	dp := plugin.NewDevicePlugin()

	if err := dp.Start(); err != nil {
		panic(err)
	}

	select {} // block forever
}
