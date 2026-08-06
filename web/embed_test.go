package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestDesktopMastActionsRemainVisible(t *testing.T) {
	data, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	baseCSS := strings.SplitN(string(data), "@media", 2)[0]
	hidden := regexp.MustCompile(`(?s)\.mast-actions\s*\{[^}]*display:\s*none`)
	if hidden.MatchString(baseCSS) {
		t.Fatal("desktop mast actions are hidden, making locale and account controls unreachable")
	}
}

func TestVisibleCopyUsesTranslationCatalog(t *testing.T) {
	data, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, marker := range []string{"const messages=", "function t("} {
		if !strings.Contains(source, marker) {
			t.Fatalf("missing centralized translation marker %q", marker)
		}
	}
	runtimeParts := strings.SplitN(source, "const state=", 2)
	if len(runtimeParts) != 2 {
		t.Fatal("translation catalog is not separated from UI runtime")
	}
	runtimeSource := runtimeParts[1]
	for _, leaked := range []string{
		">Add message<",
		">Stage action<",
		">Change password<",
		">可用版本<",
		">下载<",
		">服务地址<",
		"Human accounts and project roles.",
		"Organization identities; select an Agent",
	} {
		if strings.Contains(runtimeSource, leaked) {
			t.Errorf("visible copy bypasses translation catalog: %q", leaked)
		}
	}
}

func TestTranslationCatalogsStayInSync(t *testing.T) {
	data, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	zhStart := strings.Index(source, "zh:{")
	enStart := strings.Index(source, "\nen:{")
	catalogEnd := strings.Index(source, "}}\nconst state=")
	if zhStart < 0 || enStart < 0 || catalogEnd < 0 {
		t.Fatal("could not locate translation catalogs")
	}
	keyPattern := regexp.MustCompile(`(?:^|,|\n)\s*([A-Za-z][A-Za-z0-9]*):`)
	keys := func(section string) map[string]bool {
		result := map[string]bool{}
		for _, match := range keyPattern.FindAllStringSubmatch(section, -1) {
			result[match[1]] = true
		}
		return result
	}
	zhKeys := keys(source[zhStart+len("zh:{") : enStart])
	enKeys := keys(source[enStart+len("\nen:{") : catalogEnd])
	for locale, catalog := range map[string]map[string]bool{"zh": zhKeys, "en": enKeys} {
		prefix := "Object.assign(messages." + locale + ",{"
		if start := strings.Index(source, prefix); start >= 0 {
			if end := strings.Index(source[start:], "\n});"); end >= 0 {
				for key := range keys(source[start+len(prefix) : start+end]) {
					catalog[key] = true
				}
			}
		}
	}
	for key := range zhKeys {
		if !enKeys[key] {
			t.Errorf("English catalog is missing %q", key)
		}
	}
	for key := range enKeys {
		if !zhKeys[key] {
			t.Errorf("Chinese catalog is missing %q", key)
		}
	}
	callPattern := regexp.MustCompile(`\bt\('([^']+)'`)
	for _, match := range callPattern.FindAllStringSubmatch(source[catalogEnd:], -1) {
		if !zhKeys[match[1]] || !enKeys[match[1]] {
			t.Errorf("translation call references missing key %q", match[1])
		}
	}
}

func TestQueueAndTaskUsePrototypeStructure(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"queue-page-head", "queue-tabs", "work-main", "task-page-header", "task-meta-strip", "phase-band", "conversation-stream", "conversation-entry", "task-preview", "child-list"} {
		if !strings.Contains(string(app), marker) {
			t.Errorf("task UI is missing prototype structure %q", marker)
		}
		if !strings.Contains(string(css), "."+marker) {
			t.Errorf("task UI is missing prototype styling for %q", marker)
		}
	}
	if !strings.Contains(string(app), "data-queue-stage") {
		t.Error("task queue is missing per-stage tabs")
	}
}
