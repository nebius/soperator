package acceptance

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"

	"github.com/cucumber/godog"
)

func orderScenarioPaths(source FeatureSource, paths []string, heavyFirst bool, randomSeed int64) ([]string, error) {
	features, err := (godog.TestSuite{Options: &godog.Options{FS: source.FS, Paths: paths}}).RetrieveFeatures()
	if err != nil {
		return nil, fmt.Errorf("order scenarios: %w", err)
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
	if randomSeed != 0 {
		// Godog shuffles pickles within each feature; exact scenario selectors
		// also need their launch queue shuffled to randomize across features.
		rand.New(rand.NewSource(randomSeed)).Shuffle(len(scenarios), func(i, j int) {
			scenarios[i], scenarios[j] = scenarios[j], scenarios[i]
		})
	} else if heavyFirst {
		slices.SortStableFunc(scenarios, func(a, b selection) int {
			if a.heavy == b.heavy {
				return 0
			}
			if a.heavy {
				return -1
			}
			return 1
		})
	}
	var ordered []string
	for _, scenario := range scenarios {
		ordered = append(ordered, scenario.path)
	}
	return ordered, nil
}
