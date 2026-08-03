package versionfilter

import (
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	gherkin "github.com/cucumber/gherkin/go/v26"
	messages "github.com/cucumber/messages/go/v21"
)

var (
	baseVersionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)(?:[-+].*)?$`)
	constraintPattern  = regexp.MustCompile(`^(>=|<=|>|<|=)?(v?[0-9]+\.[0-9]+\.[0-9]+)$`)
)

// FeatureSource describes feature files to filter before Godog runs them.
type FeatureSource struct {
	FS    fs.FS
	Paths []string
}

// Axis describes one required version tag family and the target version used to
// evaluate it. For example: @soperator_version_ or @another_soperator_version_.
type Axis struct {
	TagPrefix     string
	TargetVersion string
}

type preparedAxis struct {
	tagPrefix string
	target    *semver.Version
}

type featureLocation struct {
	path string
	line int
}

type featureScenario struct {
	featurePath string
	name        string
	tags        []string
	line        int
}

// NormalizeVersion returns the major.minor.patch base version used by filters.
func NormalizeVersion(raw string) (string, error) {
	version, err := ParseBaseVersion(raw)
	if err != nil {
		return "", err
	}
	return version.String(), nil
}

// ParseBaseVersion accepts full major.minor.patch versions and strips suffixes
// from target versions such as 5.0.0-reb85d0e5.
func ParseBaseVersion(raw string) (*semver.Version, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("version is required")
	}

	matches := baseVersionPattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return nil, fmt.Errorf("version %q must use full major.minor.patch form", raw)
	}

	version, err := semver.StrictNewVersion(matches[1])
	if err != nil {
		return nil, fmt.Errorf("parse version %q: %w", raw, err)
	}
	return version, nil
}

// SelectScenarios returns Godog paths for scenarios compatible with all axes.
// Every provided axis is required: each scenario must have exactly one matching
// tag prefix for every axis.
func SelectScenarios(source FeatureSource, axes ...Axis) ([]string, error) {
	if source.FS == nil {
		return nil, fmt.Errorf("feature source FS is required")
	}

	preparedAxes, err := prepareAxes(axes)
	if err != nil {
		return nil, err
	}

	parsedByPath := make(map[string][]featureScenario)
	seen := make(map[string]struct{})
	var selected []string

	for _, rawPath := range source.Paths {
		location, err := parseFeatureLocation(rawPath)
		if err != nil {
			return nil, err
		}

		scenarios, ok := parsedByPath[location.path]
		if !ok {
			scenarios, err = parseFeatureScenarios(source.FS, location.path)
			if err != nil {
				return nil, err
			}
			if len(scenarios) == 0 {
				return nil, fmt.Errorf("feature %s does not contain scenarios", location.path)
			}
			if err := validateVersionTags(scenarios, preparedAxes); err != nil {
				return nil, err
			}
			parsedByPath[location.path] = scenarios
		}

		candidates := scenarios
		if location.line > 0 {
			candidate, ok := findScenarioStartingAtLine(scenarios, location.line)
			if !ok {
				return nil, fmt.Errorf("feature %s has no scenario starting at line %d; line-based selection must point to the Scenario line",
					location.path,
					location.line,
				)
			}
			candidates = []featureScenario{candidate}
		}

		for _, scenario := range candidates {
			compatible, err := scenarioCompatibleWithAxes(scenario, preparedAxes)
			if err != nil {
				return nil, err
			}
			if !compatible {
				continue
			}

			selectedPath := fmt.Sprintf("%s:%d", scenario.featurePath, scenario.line)
			if _, ok := seen[selectedPath]; ok {
				continue
			}
			seen[selectedPath] = struct{}{}
			selected = append(selected, selectedPath)
		}
	}

	return selected, nil
}

func prepareAxes(axes []Axis) ([]preparedAxis, error) {
	if len(axes) == 0 {
		return nil, fmt.Errorf("at least one version axis is required")
	}

	prepared := make([]preparedAxis, 0, len(axes))
	seen := make(map[string]struct{}, len(axes))
	for _, axis := range axes {
		tagPrefix := strings.TrimSpace(axis.TagPrefix)
		if tagPrefix == "" {
			return nil, fmt.Errorf("version tag prefix is required")
		}
		if !strings.HasPrefix(tagPrefix, "@") {
			return nil, fmt.Errorf("version tag prefix %q must start with @", tagPrefix)
		}
		if _, ok := seen[tagPrefix]; ok {
			return nil, fmt.Errorf("duplicate version tag prefix %q", tagPrefix)
		}
		seen[tagPrefix] = struct{}{}

		target, err := ParseBaseVersion(axis.TargetVersion)
		if err != nil {
			return nil, fmt.Errorf("target version for %s: %w", tagPrefix, err)
		}

		prepared = append(prepared, preparedAxis{
			tagPrefix: tagPrefix,
			target:    target,
		})
	}

	return prepared, nil
}

func parseFeatureLocation(rawPath string) (featureLocation, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return featureLocation{}, fmt.Errorf("feature path cannot be empty")
	}

	colon := strings.LastIndex(trimmed, ":")
	if colon == -1 {
		return featureLocation{path: trimmed}, nil
	}

	lineRaw := strings.TrimSpace(trimmed[colon+1:])
	line, err := strconv.Atoi(lineRaw)
	if err != nil || line <= 0 {
		return featureLocation{}, fmt.Errorf("feature path %q has invalid line %q", rawPath, lineRaw)
	}
	path := strings.TrimSpace(trimmed[:colon])
	if path == "" {
		return featureLocation{}, fmt.Errorf("feature path cannot be empty")
	}
	return featureLocation{path: path, line: line}, nil
}

func parseFeatureScenarios(source fs.FS, path string) ([]featureScenario, error) {
	content, err := fs.ReadFile(source, path)
	if err != nil {
		return nil, fmt.Errorf("read feature %s: %w", path, err)
	}

	document, err := gherkin.ParseGherkinDocument(bytes.NewReader(content), (&messages.Incrementing{}).NewId)
	if err != nil {
		return nil, fmt.Errorf("parse feature %s: %w", path, err)
	}
	if document.Feature == nil {
		return nil, nil
	}

	var scenarios []featureScenario
	for _, child := range document.Feature.Children {
		switch {
		case child.Scenario != nil:
			scenario, err := newFeatureScenario(path, child.Scenario)
			if err != nil {
				return nil, err
			}
			scenarios = append(scenarios, scenario)
		case child.Rule != nil:
			for _, ruleChild := range child.Rule.Children {
				if ruleChild.Scenario == nil {
					continue
				}
				scenario, err := newFeatureScenario(path, ruleChild.Scenario)
				if err != nil {
					return nil, err
				}
				scenarios = append(scenarios, scenario)
			}
		}
	}

	return scenarios, nil
}

func newFeatureScenario(path string, scenario *messages.Scenario) (featureScenario, error) {
	line, ok := lineNumber(scenario.Location)
	if !ok {
		return featureScenario{}, fmt.Errorf("scenario %q in %s has no location", scenario.Name, path)
	}

	return featureScenario{
		featurePath: path,
		name:        scenario.Name,
		tags:        tagNames(scenario.Tags),
		line:        line,
	}, nil
}

func tagNames(tags []*messages.Tag) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	return names
}

func lineNumber(location *messages.Location) (int, bool) {
	if location == nil || location.Line <= 0 {
		return 0, false
	}
	return int(location.Line), true
}

func validateVersionTags(scenarios []featureScenario, axes []preparedAxis) error {
	for _, scenario := range scenarios {
		for _, axis := range axes {
			versionTags := scenarioVersionTags(scenario, axis.tagPrefix)
			if len(versionTags) != 1 {
				return fmt.Errorf("scenario %q at %s:%d must have exactly one %s tag, got %d",
					scenario.name,
					scenario.featurePath,
					scenario.line,
					axis.tagPrefix,
					len(versionTags),
				)
			}
			// Validate tags while parsing the feature, so malformed tags fail even
			// when the scenario is incompatible with the current target version.
			if err := validateVersionConstraintExpression(strings.TrimPrefix(versionTags[0], axis.tagPrefix)); err != nil {
				return fmt.Errorf("scenario %q at %s:%d has invalid version tag %s: %w",
					scenario.name,
					scenario.featurePath,
					scenario.line,
					versionTags[0],
					err,
				)
			}
		}
	}
	return nil
}

func findScenarioStartingAtLine(scenarios []featureScenario, line int) (featureScenario, bool) {
	for _, scenario := range scenarios {
		if scenario.line == line {
			return scenario, true
		}
	}
	return featureScenario{}, false
}

func scenarioCompatibleWithAxes(scenario featureScenario, axes []preparedAxis) (bool, error) {
	for _, axis := range axes {
		versionTags := scenarioVersionTags(scenario, axis.tagPrefix)
		if len(versionTags) != 1 {
			return false, fmt.Errorf("scenario %q at %s:%d must have exactly one %s tag, got %d",
				scenario.name,
				scenario.featurePath,
				scenario.line,
				axis.tagPrefix,
				len(versionTags),
			)
		}

		expression := strings.TrimPrefix(versionTags[0], axis.tagPrefix)
		compatible, err := versionConstraintMatches(expression, axis.target)
		if err != nil {
			return false, fmt.Errorf("scenario %q at %s:%d has invalid version tag %s: %w",
				scenario.name,
				scenario.featurePath,
				scenario.line,
				versionTags[0],
				err,
			)
		}
		if !compatible {
			return false, nil
		}
	}
	return true, nil
}

func scenarioVersionTags(scenario featureScenario, tagPrefix string) []string {
	var versionTags []string
	for _, tag := range scenario.tags {
		if strings.HasPrefix(tag, tagPrefix) {
			versionTags = append(versionTags, tag)
		}
	}
	return versionTags
}

func versionConstraintMatches(expression string, target *semver.Version) (bool, error) {
	if err := validateVersionConstraintExpression(expression); err != nil {
		return false, err
	}
	constraint, err := semver.NewConstraint(expression)
	if err != nil {
		return false, err
	}
	return constraint.Check(target), nil
}

func validateVersionConstraintExpression(expression string) error {
	orGroups := strings.Split(expression, "||")
	for _, group := range orGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			return fmt.Errorf("empty version constraint group")
		}

		for _, rawConstraint := range strings.Split(group, ",") {
			constraint := strings.TrimSpace(rawConstraint)
			if constraint == "" {
				return fmt.Errorf("empty version constraint")
			}

			matches := constraintPattern.FindStringSubmatch(constraint)
			if matches == nil {
				return fmt.Errorf("constraint %q must use an optional operator and full major.minor.patch version", constraint)
			}
		}
	}
	return nil
}
