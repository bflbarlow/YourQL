package services

import (
	"fmt"
	"sort"
)

var driverRegistry = map[string]DBDriver{}

// RegisterDriver adds a DBDriver to the registry. Call in init() from each driver file.
func RegisterDriver(driver DBDriver) {
	driverRegistry[driver.TypeKey()] = driver
}

// GetDriver returns the registered driver for a given database type.
func GetDriver(dbType string) (DBDriver, error) {
	d, ok := driverRegistry[dbType]
	if !ok {
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
	return d, nil
}

// SupportedDBTypes returns the list of registered database type keys.
func SupportedDBTypes() []string {
	types := make([]string, 0, len(driverRegistry))
	for t := range driverRegistry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// DBTypeInfo describes a supported database type for the frontend.
type DBTypeInfo struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	DefaultPort int    `json:"default_port"`
}

// GetSupportedDBTypes returns metadata about all registered database types.
func GetSupportedDBTypes() []DBTypeInfo {
	infos := make([]DBTypeInfo, 0, len(driverRegistry))
	for _, d := range driverRegistry {
		infos = append(infos, DBTypeInfo{
			Type:        d.TypeKey(),
			DisplayName: d.DisplayName(),
			DefaultPort: d.DefaultPort(),
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].DisplayName < infos[j].DisplayName
	})
	return infos
}