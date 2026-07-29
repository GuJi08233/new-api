package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildDifferencesFiltersToEnabledOrLocallyConfiguredModels(t *testing.T) {
	localData := map[string]any{
		"model_ratio": map[string]float64{
			"local-model": 1,
		},
	}
	channels := []struct {
		name string
		data map[string]any
	}{
		{
			name: "upstream",
			data: map[string]any{
				"model_ratio": map[string]float64{
					"enabled-model": 2,
					"hidden-model":  3,
					"local-model":   4,
				},
			},
		},
	}

	tests := []struct {
		name             string
		enabledModelsSet map[string]struct{}
		includedModels   []string
		excludedModels   []string
	}{
		{
			name:             "filter disabled",
			enabledModelsSet: nil,
			includedModels:   []string{"enabled-model", "hidden-model", "local-model"},
		},
		{
			name:             "enabled and local models retained",
			enabledModelsSet: map[string]struct{}{"enabled-model": {}},
			includedModels:   []string{"enabled-model", "local-model"},
			excludedModels:   []string{"hidden-model"},
		},
		{
			name:             "empty enabled set still retains local models",
			enabledModelsSet: map[string]struct{}{},
			includedModels:   []string{"local-model"},
			excludedModels:   []string{"enabled-model", "hidden-model"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			differences := buildDifferences(localData, channels, test.enabledModelsSet)

			for _, modelName := range test.includedModels {
				assert.Contains(t, differences, modelName)
			}
			for _, modelName := range test.excludedModels {
				assert.NotContains(t, differences, modelName)
			}
		})
	}
}
