# Tenstorrent device plugin for Kubernetes

## Summary

This plugin adds support for Tenstorrent devices to Kubernetes and reports device into to the kubelet. See [Device Plugins](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/) for upstream documentation.

## Prerequisites

To use this device plugin, you must first have already installed `tt-kmd` on the kubernetes hosts.
See [github.com/tenstorrent/tt-kmd](http://github.com/tenstorrent/tt-kmd).

## How it works

A device plugin is a small gRPC service on each node that discovers hardware, registers custom resources with tge kubelet, and when a Pod requests those resources, provides the runtime instructions needed to attach the device to the container.

You would typically find this information from `tt-smi -ls` or in the `/dev/tenstorrent` device tree.

Conceptually, you could then tell the kubelet about that and make a request for a card to get it scheduled. That process would look like this:

```mermaid
sequenceDiagram
    participant DP as Device Plugin
    participant Kubelet
    participant Pod as Pod (Pending)

    Note over DP: 1. Device discovery<br/>List hardware on the node
    DP->>Kubelet: 2. Register(resourceName)
    Kubelet->>DP: 3. ListAndWatch()
    DP-->>Kubelet: Stream{DeviceID, Health}

    Note over Pod,Kubelet: Pod is scheduled with resource request<br/>e.g. requests: tenstorrent.com/n150: 1

    Kubelet->>DP: 4. Allocate(DeviceIDs)
    DP-->>Kubelet: Container runtime config<br/>(Device nodes, env, mounts)

    Kubelet->>Pod: 5. Start container with allocated devices
```

## Roadmap

- [ ] Enumerate the hardware
  - [x] a fake list at first
  - [ ] actual hardware
- [?] Implement the gRPC server for the Kubernetes Device Plugin API
  - [?] [Register](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#device-plugin-registration)
- [?] Register with kubelet via the Unix socket
- [?] Return something valid from `Allocate()` ()
- [ ] Test E2E ([see Example](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/#example-pod))
