package tools

import (
	"sync"
	"testing"
	"time"
)

// A hide that has expired is read by every turn, the heartbeat and the web
// server at once. Reading the registry must never write to it: two readers
// deleting the same expired entry is a concurrent map write, which Go does not
// recover from.
func TestRegistryReadsAreSafeWhenAHideExpires(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(described{"weather_now", "Weather."})
	for round := 0; round < 3000; round++ {
		reg.RegisterHidden(described{"exec", "Run a command."}, time.Nanosecond)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reg.List()
				reg.GetSummaries()
				reg.IsHidden("exec")
			}()
		}
		wg.Wait()
	}
}

// Policy is changed while turns ask whether a tool is allowed.
func TestRegistryPolicyReadsAreSafeWhileItChanges(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(described{"device", "Devices."})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				reg.AllowedFor("device", "mobile", "s1")
				reg.PolicyAllows("device", "mobile", "s1")
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				reg.SetToolEnabledForChannel("mobile", "device", j%2 == 0)
				reg.SetToolEnabledForSession("s1", "device", j%2 == 1)
			}
		}(i)
	}
	wg.Wait()
}
