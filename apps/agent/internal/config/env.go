package config

import "os"

// envLookup adapts os.LookupEnv to go.uber.org/config's expansion signature.
func envLookup(name string) (string, bool) {
	return os.LookupEnv(name)
}
