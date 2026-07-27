package model

import "strings"

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

	var configurations []Model
	if err := ReadDB().Model(&Model{}).
		Select("model_name", "name_rule").
		Where("model_name IN ? OR name_rule <> ?", models, NameRuleExact).
		Find(&configurations).Error; err != nil {
		return nil, err
	}

	exactNames := make(map[string]struct{}, len(configurations))
	ruleConfigurations := make([]Model, 0, len(configurations))
	for _, configuration := range configurations {
		exactNames[configuration.ModelName] = struct{}{}
		if configuration.NameRule != NameRuleExact {
			ruleConfigurations = append(ruleConfigurations, configuration)
		}
	}

	missing := make([]string, 0)
	for _, name := range models {
		if _, ok := exactNames[name]; ok {
			continue
		}

		configured := false
		for _, configuration := range ruleConfigurations {
			switch configuration.NameRule {
			case NameRulePrefix:
				configured = strings.HasPrefix(name, configuration.ModelName)
			case NameRuleContains:
				configured = strings.Contains(name, configuration.ModelName)
			case NameRuleSuffix:
				configured = strings.HasSuffix(name, configuration.ModelName)
			}
			if configured {
				break
			}
		}
		if !configured {
			missing = append(missing, name)
		}
	}
	return missing, nil
}
