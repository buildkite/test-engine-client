package env

import "os"

type Env interface {
	Get(key string) string
	Set(key string, value string) error
	Delete(key string) error
	Lookup(key string) (string, bool)
}

// OS is an Env backed by real operating system environment
type OS struct{}

func (OS) Get(key string) string {
	return os.Getenv(key)
}

func (OS) Set(key string, value string) error {
	return os.Setenv(key, value)
}

func (OS) Delete(key string) error {
	return os.Unsetenv(key)
}

func (OS) Lookup(key string) (string, bool) {
	return os.LookupEnv(key)
}

// Map is an Env backed by a map[string]string for testing etc
type Map map[string]string

func (env Map) Get(key string) string {
	return env[key]
}

func (env Map) Set(key string, value string) error {
	env[key] = value
	return nil
}

func (env Map) Delete(key string) error {
	delete(env, key)
	return nil
}

func (env Map) Lookup(key string) (string, bool) {
	if val, ok := env[key]; ok {
		return val, true
	} else {
		return "", false
	}
}
