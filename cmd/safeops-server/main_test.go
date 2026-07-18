package main

import "testing"

func TestValidateListenAddressAllowsOnlyLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if err := validateListenAddress(address); err != nil {
			t.Fatalf("loopback address %s rejected: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", ":8080", "192.168.1.10:8080", "safeops.example:8080", "not-an-address"} {
		if err := validateListenAddress(address); err == nil {
			t.Fatalf("non-loopback address %s accepted", address)
		}
	}
}
