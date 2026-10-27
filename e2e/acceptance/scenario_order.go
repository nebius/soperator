package acceptance

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cucumber/godog"
)

func heavyScenariosFirst(source FeatureSource, paths []string) ([]string, error) {
	features, err := (godog.TestSuite{Options: &godog.Options{FS: source.FS, Paths: paths}}).RetrieveFeatures()
	if err != nil {
		return nil, fmt.Errorf("order heavy scenarios: %w", err)
	}
	type selection struct {
		path  string
		heavy bool
	}
	var scenarios []selection
	indexByPath := make(map[string]int)
	for _, feature := range features {
		for _, pickle := range feature.Pickles {
			scenario := feature.FindScenario(pickle.AstNodeIds[0])
			suffix := fmt.Sprintf(":%d", scenario.Location.Line)
			location := strings.TrimSuffix(feature.Uri, suffix) + suffix
			index, exists := indexByPath[location]
			if !exists {
				index = len(scenarios)
				indexByPath[location] = index
				scenarios = append(scenarios, selection{path: location})
			}
			for _, tag := range pickle.Tags {
				if tag.Name == "@heavy" {
					scenarios[index].heavy = true
				}
			}
		}
	}
	slices.SortStableFunc(scenarios, func(a, b selection) int {
		if a.heavy == b.heavy {
			return 0
		}
		if a.heavy {
			return -1
		}
		return 1
	})
	var ordered []string
	for _, scenario := range scenarios {
		ordered = append(ordered, scenario.path)
	}
	return ordered, nil
}
