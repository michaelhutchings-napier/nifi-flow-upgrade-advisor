package flowupgrade

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

var (
	allowedRuleCategories = map[string]struct{}{
		"component-removed":      {},
		"component-replaced":     {},
		"bundle-renamed":         {},
		"property-renamed":       {},
		"property-value-changed": {},
		"property-removed":       {},
		"variable-migration":     {},
		"manual-inspection":      {},
		"blocked":                {},
	}
	allowedFindingClasses = map[string]struct{}{
		"auto-fix":          {},
		"manual-change":     {},
		"manual-inspection": {},
		"blocked":           {},
		"info":              {},
	}
	allowedSeverities = map[string]struct{}{
		"info":    {},
		"warning": {},
		"error":   {},
	}
	allowedScopes = map[string]struct{}{
		"processor":          {},
		"controller-service": {},
		"reporting-task":     {},
		"parameter-context":  {},
		"flow-root":          {},
	}
	allowedActionTypes = map[string]struct{}{
		"rename-property":          {},
		"set-property":             {},
		"remove-property":          {},
		"replace-property-value":   {},
		"update-bundle-coordinate": {},
		"replace-component-type":   {},
		"emit-parameter-scaffold":  {},
		"mark-blocked":             {},
	}
)

func LoadRulePacks(paths []string) ([]RulePack, error) {
	if len(paths) == 0 {
		return nil, newExitError(exitCodeUsage, "at least one --rule-pack is required")
	}

	packs := make([]RulePack, 0, len(paths))
	for _, path := range paths {
		pack, err := loadRulePack(path)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}

	if err := validateRulePackSet(packs); err != nil {
		return nil, err
	}

	return packs, nil
}

func loadRulePack(path string) (RulePack, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return RulePack{}, newExitError(exitCodeRulePackInvalid, "read rule pack %q: %v", path, err)
	}

	var pack RulePack
	if err := yaml.Unmarshal(content, &pack); err != nil {
		return RulePack{}, newExitError(exitCodeRulePackInvalid, "parse rule pack %q: %v", path, err)
	}
	pack.Path = path

	if err := validateRulePack(pack); err != nil {
		return RulePack{}, newExitError(exitCodeRulePackInvalid, "invalid rule pack %q: %v", path, err)
	}

	return pack, nil
}

func validateRulePackSet(packs []RulePack) error {
	seenNames := map[string]string{}
	seenRuleIDs := map[string]string{}

	for _, pack := range packs {
		if prior, ok := seenNames[pack.Metadata.Name]; ok {
			return fmt.Errorf("duplicate rule pack name %q in %q and %q", pack.Metadata.Name, prior, pack.Path)
		}
		seenNames[pack.Metadata.Name] = pack.Path

		for _, rule := range pack.Spec.Rules {
			if prior, ok := seenRuleIDs[rule.ID]; ok {
				return fmt.Errorf("duplicate rule id %q in %q and %q", rule.ID, prior, pack.Path)
			}
			seenRuleIDs[rule.ID] = pack.Path
		}
	}

	return nil
}

func validateRulePack(pack RulePack) error {
	if pack.APIVersion != reportAPIVersion {
		return fmt.Errorf("apiVersion must be %q", reportAPIVersion)
	}
	if pack.Kind != rulePackKind {
		return fmt.Errorf("kind must be %q", rulePackKind)
	}
	if strings.TrimSpace(pack.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if _, err := semver.NewConstraint(pack.Spec.SourceVersionRange); err != nil {
		return fmt.Errorf("invalid sourceVersionRange: %w", err)
	}
	if _, err := semver.NewConstraint(pack.Spec.TargetVersionRange); err != nil {
		return fmt.Errorf("invalid targetVersionRange: %w", err)
	}
	for _, format := range pack.Spec.AppliesToFormats {
		if _, ok := allowedSourceFormats[format]; !ok || format == SourceFormatAuto {
			return fmt.Errorf("unsupported appliesToFormats value %q", format)
		}
	}
	if len(pack.Spec.Rules) == 0 {
		return fmt.Errorf("spec.rules must not be empty")
	}

	for _, rule := range pack.Spec.Rules {
		if err := validateRule(rule); err != nil {
			return fmt.Errorf("rule %q: %w", rule.ID, err)
		}
	}

	return nil
}

func validateRule(rule Rule) error {
	if strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if _, ok := allowedRuleCategories[rule.Category]; !ok {
		return fmt.Errorf("unsupported category %q", rule.Category)
	}
	if _, ok := allowedFindingClasses[rule.Class]; !ok {
		return fmt.Errorf("unsupported class %q", rule.Class)
	}
	if _, ok := allowedSeverities[rule.Severity]; !ok {
		return fmt.Errorf("unsupported severity %q", rule.Severity)
	}
	if strings.TrimSpace(rule.Message) == "" {
		return fmt.Errorf("message is required")
	}
	if !ruleHasNarrowing(rule) {
		return fmt.Errorf("selector or match must narrow the rule")
	}
	if rule.Selector.Scope != "" {
		if _, ok := allowedScopes[rule.Selector.Scope]; !ok {
			return fmt.Errorf("unsupported scope %q", rule.Selector.Scope)
		}
	}
	if rule.Match.ComponentNameRegex != "" {
		if _, err := regexp.Compile(rule.Match.ComponentNameRegex); err != nil {
			return fmt.Errorf("invalid componentNameMatches regex: %w", err)
		}
	}
	if rule.Match.PropertyValueEquals != nil {
		if strings.TrimSpace(rule.Match.PropertyValueEquals.Property) == "" || strings.TrimSpace(rule.Match.PropertyValueEquals.Value) == "" {
			return fmt.Errorf("propertyValueEquals requires property and value")
		}
	}
	if rule.Match.PropertyValueIn != nil {
		if strings.TrimSpace(rule.Match.PropertyValueIn.Property) == "" || len(rule.Match.PropertyValueIn.Values) == 0 {
			return fmt.Errorf("propertyValueIn requires property and at least one value")
		}
	}
	if rule.Match.PropertyValueRegex != nil {
		if strings.TrimSpace(rule.Match.PropertyValueRegex.Property) == "" || strings.TrimSpace(rule.Match.PropertyValueRegex.Regex) == "" {
			return fmt.Errorf("propertyValueRegex requires property and regex")
		}
		if _, err := regexp.Compile(rule.Match.PropertyValueRegex.Regex); err != nil {
			return fmt.Errorf("invalid propertyValueRegex regex: %w", err)
		}
	}
	for _, action := range rule.Actions {
		if err := validateAction(action); err != nil {
			return err
		}
	}
	return nil
}

func validateAction(action RuleAction) error {
	if _, ok := allowedActionTypes[action.Type]; !ok {
		return fmt.Errorf("unsupported action type %q", action.Type)
	}
	switch action.Type {
	case "rename-property", "replace-component-type":
		if strings.TrimSpace(action.From) == "" || strings.TrimSpace(action.To) == "" {
			return fmt.Errorf("%s requires from and to", action.Type)
		}
	case "set-property":
		if strings.TrimSpace(action.Property) == "" || action.Value == "" {
			return fmt.Errorf("set-property requires property and value")
		}
	case "remove-property":
		if strings.TrimSpace(action.Name) == "" {
			return fmt.Errorf("remove-property requires name")
		}
	case "replace-property-value":
		if strings.TrimSpace(action.Property) == "" || strings.TrimSpace(action.From) == "" || strings.TrimSpace(action.To) == "" {
			return fmt.Errorf("replace-property-value requires property, from, and to")
		}
	case "update-bundle-coordinate":
		if strings.TrimSpace(action.Group) == "" || strings.TrimSpace(action.Artifact) == "" {
			return fmt.Errorf("update-bundle-coordinate requires group and artifact")
		}
	case "emit-parameter-scaffold":
		if strings.TrimSpace(action.ParameterName) == "" {
			return fmt.Errorf("emit-parameter-scaffold requires parameterName")
		}
	case "mark-blocked":
	}
	return nil
}

func ruleHasNarrowing(rule Rule) bool {
	if strings.TrimSpace(rule.Selector.ComponentType) != "" ||
		len(rule.Selector.ComponentTypes) > 0 ||
		strings.TrimSpace(rule.Selector.BundleGroup) != "" ||
		strings.TrimSpace(rule.Selector.BundleArtifact) != "" ||
		strings.TrimSpace(rule.Selector.PropertyName) != "" ||
		strings.TrimSpace(rule.Selector.Scope) != "" {
		return true
	}
	return strings.TrimSpace(rule.Match.PropertyExists) != "" ||
		strings.TrimSpace(rule.Match.PropertyAbsent) != "" ||
		rule.Match.PropertyValueEquals != nil ||
		rule.Match.PropertyValueIn != nil ||
		rule.Match.PropertyValueRegex != nil ||
		strings.TrimSpace(rule.Match.AnnotationContains) != "" ||
		strings.TrimSpace(rule.Match.ComponentNameRegex) != ""
}

func filterMatchingRulePacks(packs []RulePack, sourceVersion, targetVersion string, format SourceFormat) ([]RulePack, error) {
	source, err := semver.NewVersion(sourceVersion)
	if err != nil {
		return nil, newExitError(exitCodeUsage, "invalid --source-version %q: %v", sourceVersion, err)
	}
	target, err := semver.NewVersion(targetVersion)
	if err != nil {
		return nil, newExitError(exitCodeUsage, "invalid --target-version %q: %v", targetVersion, err)
	}

	var matched []RulePack
	for _, pack := range packs {
		sourceConstraint, _ := semver.NewConstraint(pack.Spec.SourceVersionRange)
		targetConstraint, _ := semver.NewConstraint(pack.Spec.TargetVersionRange)
		if !sourceConstraint.Check(source) || !targetConstraint.Check(target) {
			continue
		}
		if len(pack.Spec.AppliesToFormats) > 0 && !slices.Contains(pack.Spec.AppliesToFormats, format) {
			continue
		}
		matched = append(matched, pack)
	}
	return matched, nil
}
