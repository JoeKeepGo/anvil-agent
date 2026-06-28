package lifecycle

import (
	"context"
	"net/http"
	"os"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

type ReadinessDetector struct {
	incus        IncusBackend
	kvmPath      string
	kvmAvailable func(string) (bool, error)
}

func NewReadinessDetector(backend IncusBackend) *ReadinessDetector {
	return &ReadinessDetector{incus: backend, kvmPath: "/dev/kvm", kvmAvailable: defaultKVMAvailable}
}

func (d *ReadinessDetector) VMLifecycleAvailable(ctx context.Context) (bool, error) {
	if d == nil || d.incus == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	kvmPath := d.kvmPath
	if kvmPath == "" {
		kvmPath = "/dev/kvm"
	}
	kvmAvailable := d.kvmAvailable
	if kvmAvailable == nil {
		kvmAvailable = defaultKVMAvailable
	}
	available, err := kvmAvailable(kvmPath)
	if err != nil {
		return false, err
	}
	if !available {
		return false, nil
	}

	resp := d.incus.Execute(ctx, &incus.ProxyRequest{
		Method: http.MethodGet,
		Path:   "/1.0/profiles/default",
	})
	if resp == nil || resp.Status < 200 || resp.Status >= 300 {
		return false, nil
	}
	root, lifecycleErr := rootDiskFromDefaultProfileResponse(resp)
	return lifecycleErr == nil && root.Device["pool"] != "", nil
}

func defaultKVMAvailable(kvmPath string) (bool, error) {
	info, err := os.Stat(kvmPath)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return false, nil
	}
	return true, nil
}
