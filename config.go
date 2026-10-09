package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type config struct {
	Server   string `json:"server"`
	CACert   string `json:"cacert"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func applyConfig(o *options, path string, explicit map[string]bool) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit["config"] {
			return nil
		}
		return fmt.Errorf("cannot read config file: %w", err)
	}
	defer file.Close()
	var defaults *config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&defaults); err != nil || defaults == nil {
		return fmt.Errorf("config must be a JSON object with string values")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("config contains invalid trailing JSON")
	}
	if !explicit["server"] {
		o.server = defaults.Server
	}
	if !explicit["cacert"] {
		var missing bool
		o.caCert = os.Expand(defaults.CACert, func(name string) string {
			value, found := os.LookupEnv(name)
			if !found {
				missing = true
			}
			return value
		})
		if missing {
			return fmt.Errorf("configured cacert references an unset environment variable")
		}
	}
	if o.command == "login" {
		if !explicit["email"] {
			o.email = defaults.Email
		}
		if !explicit["password"] {
			o.password = defaults.Password
		}
	}
	return nil
}
