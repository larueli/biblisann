// Package config loads the service configuration from environment variables and an optional
// dotenv-style file.
//
// Every value has an environment variable BIBLISANN_<NAME>. Sensitive values also accept
// BIBLISANN_<NAME>_FILE, which holds the path of a file containing the value. Giving the same
// value through two sources (variable, _FILE variable or config file) is an error, and error
// messages never contain values.
package config
