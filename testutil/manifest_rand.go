package testutil

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ovrclk/akash/manifest"
)

type manifestGeneratorRand struct{}

func (mg manifestGeneratorRand) Manifest(t testing.TB) manifest.Manifest {
	t.Helper()
	return []manifest.Group{
		mg.Group(t),
	}
}

func (mg manifestGeneratorRand) Group(t testing.TB) manifest.Group {
	t.Helper()
	return manifest.Group{
		Name: "left-coast",
		Services: []manifest.Service{
			mg.Service(t),
		},
	}
}

func (mg manifestGeneratorRand) Service(t testing.TB) manifest.Service {
	t.Helper()
	return manifest.Service{
		Name:  "demo",
		Image: "quay.io/ovrclk/demo-app",
		Args:  []string{"run"},
		Env:   []string{"AKASH_TEST_SERVICE=true"},
		Unit:  Unit(t),
		Count: rand.Uint32(),
		Expose: []manifest.ServiceExpose{
			mg.ServiceExpose(t),
		},
	}
}

func (mg manifestGeneratorRand) ServiceExpose(_ testing.TB) manifest.ServiceExpose {
	return manifest.ServiceExpose{
		Port:         uint16(rand.Intn(math.MaxUint16)),
		ExternalPort: uint16(rand.Intn(math.MaxUint16)),
		Proto:        "http",
		Service:      "svc",
		Global:       true,
		Hosts: []string{
			"foo.com",
		},
	}
}
