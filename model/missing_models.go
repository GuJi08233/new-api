package model

// GetMissingModels returns model names that are referenced in the system but
// do not yet have a corresponding entry in the models meta table.
//
// When group is empty the lookup spans every enabled ability (global view).
// When group is provided (and not the synthetic "global" alias) only models
// enabled for that group are considered, so callers can ask "which models are
// missing for group X" without leaking models exposed under other groups.
func GetMissingModels(group string) ([]string, error) {
	var models []string
	if group == "" || group == "global" {
		models = GetEnabledModels()
	} else {
		models = GetGroupEnabledModels(group)
	}
	if len(models) == 0 {
		return []string{}, nil
	}

	var existing []string
	if err := ReadDB().Model(&Model{}).Where("model_name IN ?", models).Pluck("model_name", &existing).Error; err != nil {
		return nil, err
	}

	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	missing := make([]string, 0)
	for _, name := range models {
		if _, ok := existingSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing, nil
}
