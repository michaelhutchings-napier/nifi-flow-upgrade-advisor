package flowupgrade

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const extensionsManifestKind = "ExtensionsManifest"

func LoadExtensionsManifest(path string) (*ExtensionsManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, newExitError(exitCodeSourceRead, "read extensions manifest %q: %v", path, err)
	}

	var manifest ExtensionsManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, newExitError(exitCodeSourceRead, "parse extensions manifest %q: %v", path, err)
	}
	manifest.Path = path

	if err := validateExtensionsManifest(manifest); err != nil {
		return nil, newExitError(exitCodeUsage, "invalid extensions manifest %q: %v", path, err)
	}

	return &manifest, nil
}

func validateExtensionsManifest(manifest ExtensionsManifest) error {
	if manifest.APIVersion != reportAPIVersion {
		return fmt.Errorf("apiVersion must be %q", reportAPIVersion)
	}
	if manifest.Kind != extensionsManifestKind {
		return fmt.Errorf("kind must be %q", extensionsManifestKind)
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if len(manifest.Spec.Components) == 0 {
		return fmt.Errorf("spec.components must not be empty")
	}

	seen := map[string]struct{}{}
	for _, component := range manifest.Spec.Components {
		if strings.TrimSpace(component.Type) == "" {
			return fmt.Errorf("spec.components[].type is required")
		}
		if component.Scope != "" {
			if _, ok := allowedScopes[component.Scope]; !ok {
				return fmt.Errorf("unsupported spec.components[].scope %q", component.Scope)
			}
		}
		key := component.Scope + "|" + component.Type
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate component entry for scope %q and type %q", component.Scope, component.Type)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func (manifest ExtensionsManifest) HasComponent(component FlowComponent) bool {
	for _, candidate := range manifest.Spec.Components {
		if candidate.Type != component.Type {
			continue
		}
		if candidate.Scope != "" && candidate.Scope != component.Scope {
			continue
		}
		if candidate.BundleGroup != "" && candidate.BundleGroup != component.BundleGroup {
			continue
		}
		if candidate.BundleArtifact != "" && candidate.BundleArtifact != component.BundleArtifact {
			continue
		}
		return true
	}
	return false
}

func extensionsManifestFindings(document FlowDocument, packs []RulePack, manifest *ExtensionsManifest) []MigrationFinding {
	if manifest == nil {
		return nil
	}

	findings := make([]MigrationFinding, 0)
	for i := range document.Components {
		component := document.Components[i]
		targetComponent := plannedTargetComponent(component, document, packs)
		if manifest.HasComponent(targetComponent) {
			continue
		}
		findings = append(findings, buildExtensionsManifestFinding(component, targetComponent, manifest))
	}
	return findings
}

func plannedTargetComponent(component FlowComponent, document FlowDocument, packs []RulePack) FlowComponent {
	planned := component
	for _, pack := range packs {
		for _, rule := range pack.Spec.Rules {
			if rule.Class != "auto-fix" {
				continue
			}
			_, matched := ruleMatchesComponent(rule, component, document)
			if !matched {
				continue
			}
			for _, action := range rule.Actions {
				switch action.Type {
				case "replace-component-type":
					if action.From == "" || action.From == planned.Type {
						planned.Type = action.To
					}
				case "update-bundle-coordinate":
					if action.Group != "" {
						planned.BundleGroup = action.Group
					}
					if action.Artifact != "" {
						planned.BundleArtifact = action.Artifact
					}
				}
			}
		}
	}
	return planned
}

func buildExtensionsManifestFinding(sourceComponent, targetComponent FlowComponent, manifest *ExtensionsManifest) MigrationFinding {
	message := fmt.Sprintf("Target extensions manifest does not include a supported component for %s.", sourceComponent.Type)
	notes := fmt.Sprintf("Review the target NiFi extension inventory in %s. The planned target component type is %s.", manifest.Path, targetComponent.Type)
	evidence := []FindingEvidence{
		{
			Type:        "target-extensions-manifest",
			Field:       "sourceType",
			ActualValue: sourceComponent.Type,
		},
		{
			Type:        "target-extensions-manifest",
			Field:       "plannedTargetType",
			ActualValue: targetComponent.Type,
		},
	}
	if targetComponent.BundleArtifact != "" || targetComponent.BundleGroup != "" {
		evidence = append(evidence, FindingEvidence{
			Type:        "target-extensions-manifest",
			Field:       "plannedTargetBundle",
			ActualValue: firstNonEmpty(targetComponent.BundleGroup, "") + ":" + firstNonEmpty(targetComponent.BundleArtifact, ""),
		})
	}
	if sourceComponent.Type != targetComponent.Type {
		message = fmt.Sprintf("Target extensions manifest does not include the planned replacement type %s.", targetComponent.Type)
	}

	return MigrationFinding{
		RuleID:     "system.target-extension-unavailable",
		Class:      "blocked",
		Severity:   "error",
		Message:    message,
		Notes:      notes,
		References: []string{manifest.Path},
		Evidence:   evidence,
		Component: &FindingComponent{
			ID:    sourceComponent.ID,
			Name:  sourceComponent.Name,
			Type:  sourceComponent.Type,
			Scope: sourceComponent.Scope,
			Path:  sourceComponent.Path,
		},
	}
}
