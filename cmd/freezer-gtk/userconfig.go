// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/shibukawa/configdir"
)

const (
	vendorName     = "filefreezer"
	applcationName = "filefreezer"
	configFilename = "userconfig.json"
)

// DirectoryMapping represents the logical connection between a local directory
// and a string prefix used to organize the files on the server
type DirectoryMapping struct {
	LocalDirectory string
	RemotePrefix   string
}

// UserConfig is the user's configuration information for the filefreezer
// gtk client.
type UserConfig struct {
	// all of the servers known to the user
	ServerConnectionInfos []ServerConnectInfo
}

// ServerConnectInfo contains information needed to make connections to a
// filefreezer server. Using this, the user may setup multiple server connections
// and save them in the configuration.
type ServerConnectInfo struct {
	FriendlyName string
	URL          string
	Username     string
	Password     string // can be empty -- if so prompt user at runtime
	CryptoPass   string // can be empty -- if so prompt user at runtime
	Mappings     []DirectoryMapping
}

// NewUserConfig creates a new user configuration object.
func NewUserConfig() *UserConfig {
	cfg := new(UserConfig)
	cfg.ServerConnectionInfos = make([]ServerConnectInfo, 0)
	return cfg
}

// LoadDefaultUserConfigFile loads the user's configuration file from
// the default location for the operating system currently in use.
// If the configuraiton file cannot be found then a new UserConfig
// object is created with defaults and returned.
func LoadDefaultUserConfigFile() (*UserConfig, error) {
	// setup OS-dependent search paths for the user configuration
	configDirs := configdir.New(vendorName, applcationName)
	configDirs.LocalPath, _ = filepath.Abs(".")
	folder := configDirs.QueryFolderContainsFile(configFilename)
	if folder != nil {
		var config UserConfig
		data, err := folder.ReadFile(configFilename)
		if err != nil {
			return nil, fmt.Errorf("failed to read the user configuration in %s: %v", folder.Path, err)
		}
		json.Unmarshal(data, &config)
		return &config, nil
	}

	return NewUserConfig(), nil
}

// Save the configuration file to the user's configuration directory.
func (c *UserConfig) Save() error {
	// marshal out the configuration to bytes
	cfgBytes, err := json.MarshalIndent(*c, "", "\t")
	if err != nil {
		return fmt.Errorf("unable to make the configuration file: %v", err)
	}

	// store the data in a configuration file in the user's folder
	configDirs := configdir.New(vendorName, applcationName)
	folders := configDirs.QueryFolders(configdir.Global)
	if len(folders) < 1 {
		return fmt.Errorf("unable to find the user directory to save the configuration to")
	}

	err = folders[0].WriteFile(configFilename, cfgBytes)
	if err != nil {
		return fmt.Errorf("failed to write the configuration file: %v", err)
	}

	return nil
}
