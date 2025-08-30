package webhooks

import "BackendTemplate/pkg/database"

func CheckEnable() (exist bool, key string) {
	var Setting database.Settings
	exists, err := database.Engine.Where("name=?", "wecom").Get(&Setting)
	if !exists || err != nil {
		return false, ""
	}
	if Setting.Value == "" {
		return false, ""
	}
	return true, Setting.Value
}
