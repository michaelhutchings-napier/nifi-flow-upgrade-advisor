package flowupgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

var (
	rulePackBlockedBridgePattern = regexp.MustCompile(`^nifi-(\d+)\.(\d+)-to-(\d+)\.(\d+)-pre-(\d+)\.(\d+)\.blocked\.ya?ml$`)
	rulePackPatchPattern         = regexp.MustCompile(`^nifi-(\d+)\.(\d+)-to-(\d+)\.(\d+)\.(\d+)\.patch-caveats\.ya?ml$`)
	rulePackEdgePattern          = regexp.MustCompile(`^nifi-(\d+)\.(\d+)-to-(\d+)\.(\d+)\.(?:official|inferred)\.ya?ml$`)
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
		"assisted-rewrite":  {},
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
		"set-property-if-absent":   {},
		"copy-property":            {},
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
	case "set-property-if-absent":
		if strings.TrimSpace(action.Property) == "" || action.Value == "" {
			return fmt.Errorf("set-property-if-absent requires property and value")
		}
	case "copy-property":
		if strings.TrimSpace(action.From) == "" || strings.TrimSpace(action.To) == "" {
			return fmt.Errorf("copy-property requires from and to")
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

	if containsBlockedBridgePack(matched) {
		return matched, nil
	}
	if len(matched) > 0 {
		return matched, nil
	}

	chained, ok := selectRulePackChain(packs, source, target, format)
	if ok {
		return chained, nil
	}

	return nil, nil
}

type rulePackRouteKind string

const (
	rulePackRouteEdge          rulePackRouteKind = "edge"
	rulePackRoutePatchCaveat   rulePackRouteKind = "patch-caveat"
	rulePackRouteBlockedBridge rulePackRouteKind = "blocked-bridge"
)

type rulePackRoute struct {
	kind             rulePackRouteKind
	pack             RulePack
	fromMajor        int64
	fromMinor        int64
	toMajor          int64
	toMinor          int64
	toPatch          int64
	blockSourceEndMA int64
	blockSourceEndMI int64
}

func containsBlockedBridgePack(packs []RulePack) bool {
	for _, pack := range packs {
		if route, ok := parseRulePackRoute(pack); ok && route.kind == rulePackRouteBlockedBridge {
			return true
		}
	}
	return false
}

func selectRulePackChain(packs []RulePack, source, target *semver.Version, format SourceFormat) ([]RulePack, bool) {
	routes := make([]rulePackRoute, 0, len(packs))
	for _, pack := range packs {
		if len(pack.Spec.AppliesToFormats) > 0 && !slices.Contains(pack.Spec.AppliesToFormats, format) {
			continue
		}
		route, ok := parseRulePackRoute(pack)
		if !ok {
			continue
		}
		routes = append(routes, route)
	}

	selected := make([]RulePack, 0)

	for _, route := range routes {
		if route.kind != rulePackRouteBlockedBridge {
			continue
		}
		if compareMinor(int64(source.Major()), int64(source.Minor()), route.fromMajor, route.fromMinor) >= 0 &&
			compareMinor(int64(source.Major()), int64(source.Minor()), route.blockSourceEndMA, route.blockSourceEndMI) <= 0 &&
			int64(target.Major()) >= route.toMajor &&
			(int64(target.Major()) > route.toMajor || int64(target.Minor()) >= route.toMinor) {
			selected = append(selected, route.pack)
			return dedupeRulePacks(selected), true
		}
	}

	edgesByMinor := map[string][]rulePackRoute{}
	for _, route := range routes {
		if route.kind != rulePackRouteEdge {
			continue
		}
		key := minorKey(route.fromMajor, route.fromMinor)
		edgesByMinor[key] = append(edgesByMinor[key], route)
	}

	type chainNode struct {
		major int64
		minor int64
		path  []RulePack
	}

	startKey := minorKey(int64(source.Major()), int64(source.Minor()))
	targetKey := minorKey(int64(target.Major()), int64(target.Minor()))
	queue := []chainNode{{major: int64(source.Major()), minor: int64(source.Minor()), path: nil}}
	seen := map[string]bool{startKey: true}
	var found []RulePack

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if minorKey(current.major, current.minor) == targetKey {
			found = current.path
			break
		}
		for _, edge := range edgesByMinor[minorKey(current.major, current.minor)] {
			nextKey := minorKey(edge.toMajor, edge.toMinor)
			if seen[nextKey] {
				continue
			}
			if compareMinor(edge.toMajor, edge.toMinor, int64(target.Major()), int64(target.Minor())) > 0 {
				continue
			}
			seen[nextKey] = true
			nextPath := append(append([]RulePack{}, current.path...), edge.pack)
			queue = append(queue, chainNode{
				major: edge.toMajor,
				minor: edge.toMinor,
				path:  nextPath,
			})
		}
	}

	if len(found) == 0 {
		return nil, false
	}
	selected = append(selected, found...)

	for _, route := range routes {
		if route.kind != rulePackRoutePatchCaveat {
			continue
		}
		if int64(source.Major()) == route.fromMajor &&
			int64(source.Minor()) == route.fromMinor &&
			int64(target.Major()) == route.toMajor &&
			int64(target.Minor()) == route.toMinor &&
			int64(target.Patch()) == route.toPatch {
			selected = append(selected, route.pack)
		}
	}

	return dedupeRulePacks(selected), true
}

func dedupeRulePacks(packs []RulePack) []RulePack {
	result := make([]RulePack, 0, len(packs))
	seen := map[string]bool{}
	for _, pack := range packs {
		if seen[pack.Path] {
			continue
		}
		seen[pack.Path] = true
		result = append(result, pack)
	}
	return result
}

func parseRulePackRoute(pack RulePack) (rulePackRoute, bool) {
	name := filepath.Base(pack.Path)

	if matches := rulePackBlockedBridgePattern.FindStringSubmatch(name); len(matches) == 7 {
		return rulePackRoute{
			kind:             rulePackRouteBlockedBridge,
			pack:             pack,
			fromMajor:        parseInt64(matches[1]),
			fromMinor:        parseInt64(matches[2]),
			blockSourceEndMA: parseInt64(matches[3]),
			blockSourceEndMI: parseInt64(matches[4]),
			toMajor:          parseInt64(matches[5]),
			toMinor:          parseInt64(matches[6]),
		}, true
	}
	if matches := rulePackPatchPattern.FindStringSubmatch(name); len(matches) == 6 {
		return rulePackRoute{
			kind:      rulePackRoutePatchCaveat,
			pack:      pack,
			fromMajor: parseInt64(matches[1]),
			fromMinor: parseInt64(matches[2]),
			toMajor:   parseInt64(matches[3]),
			toMinor:   parseInt64(matches[4]),
			toPatch:   parseInt64(matches[5]),
		}, true
	}
	if matches := rulePackEdgePattern.FindStringSubmatch(name); len(matches) == 5 {
		return rulePackRoute{
			kind:      rulePackRouteEdge,
			pack:      pack,
			fromMajor: parseInt64(matches[1]),
			fromMinor: parseInt64(matches[2]),
			toMajor:   parseInt64(matches[3]),
			toMinor:   parseInt64(matches[4]),
		}, true
	}
	return rulePackRoute{}, false
}

func parseInt64(value string) int64 {
	number, _ := strconv.ParseInt(value, 10, 64)
	return number
}

func minorKey(major, minor int64) string {
	return fmt.Sprintf("%d.%d", major, minor)
}

func compareMinor(leftMajor, leftMinor, rightMajor, rightMinor int64) int {
	if leftMajor != rightMajor {
		if leftMajor < rightMajor {
			return -1
		}
		return 1
	}
	if leftMinor < rightMinor {
		return -1
	}
	if leftMinor > rightMinor {
		return 1
	}
	return 0
}

func lintRulePackWarnings(pack RulePack) []RulePackLintWarning {
	warnings := make([]RulePackLintWarning, 0)
	for _, rule := range pack.Spec.Rules {
		if strings.TrimSpace(rule.Match.PropertyExists) == "" {
			continue
		}
		warnings = append(warnings, RulePackLintWarning{
			RulePackName: pack.Metadata.Name,
			RulePackPath: pack.Path,
			RuleID:       rule.ID,
			Message:      fmt.Sprintf("rule uses match.propertyExists for %q; exported flow JSON can preserve null placeholder properties, which may cause false positives. Prefer propertyValueRegex when the rule requires a real configured value", rule.Match.PropertyExists),
		})
	}
	return warnings
}
