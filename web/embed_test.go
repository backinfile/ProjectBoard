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
	source = strings.ReplaceAll(source, "\r\n", "\n")
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
	source = strings.ReplaceAll(source, "\r\n", "\n")
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
	assignments := regexp.MustCompile(`(?s)Object\.assign\(messages\.(zh|en),\{(.*?)\}\);`)
	for _, match := range assignments.FindAllStringSubmatch(source, -1) {
		catalog := map[string]map[string]bool{"zh": zhKeys, "en": enKeys}[match[1]]
		for key := range keys(match[2]) {
			catalog[key] = true
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

func TestTaskDetailSupportsSidebarDescriptionAttachmentsAndTextPreview(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"task-basic-panel", "task-detail-grid", "task-conversation-scroll", "taskDescriptionAttachmentInput", "readableAttachmentKind", "data-preview-attachment"} {
		if !strings.Contains(string(app), marker) {
			t.Errorf("task detail UI is missing %q", marker)
		}
	}
	for _, marker := range []string{".task-basic-panel", ".task-detail-grid", ".description-attachment-picker", ".text-attachment-preview", ".composer-main:focus-within"} {
		if !strings.Contains(string(css), marker) {
			t.Errorf("task detail styling is missing %q", marker)
		}
	}
}

func TestFinalTaskRendererHandlesClosedTasksAndEnterToSend(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	if !strings.Contains(app, "const form=$('#chatComposer');if(!form)return;const toolbar=$('.composer-toolbar',form)") {
		t.Error("task renderer queries inside a missing closed-task composer")
	}
	if !strings.Contains(app, "message.addEventListener('keydown'") || !strings.Contains(app, "form.requestSubmit()") {
		t.Error("task composer does not submit with Enter")
	}
}

func TestProjectManagementDrawerOwnsProjectSettings(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(app)
	for _, marker := range []string{
		"accountProfileDrawer",
		"projectPromptSegmentDrawer",
		"projectBranchPolicyDrawer",
		"systemAddressDrawer",
		"projects-basic-list",
		"project-management-drawer",
		"data-managed-project-tab",
		"managedProjectPanel",
		"settings-reference-topbar",
		"settings-reference-tabs",
		"promptSegmentTitle",
		"currentRouteHash",
		"applyRouteFromURL",
	} {
		if !strings.Contains(source, marker) {
			t.Errorf("settings UI is missing drawer-based structure %q", marker)
		}
	}
	for _, marker := range []string{".settings-page-shell", ".project-settings-layout", ".project-prompt-segment", ".settings-reference-content", ".settings-card-icon"} {
		if !strings.Contains(string(css), marker) {
			t.Errorf("settings UI is missing styling for %q", marker)
		}
	}
	projectNav := regexp.MustCompile(`(?m)const projectNav=([^\r\n]+)`)
	matches := projectNav.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		t.Fatal("project navigation definition is missing")
	}
	for _, match := range matches {
		if strings.Contains(match[1], "'projectSettings'") {
			t.Error("project navigation still exposes the removed project settings page")
		}
		if strings.Contains(match[1], "'members'") || strings.Contains(match[1], "'agents'") {
			t.Error("members and Agents must live inside project settings, not the project sidebar")
		}
	}
	for _, forbidden := range []string{"systemAddressForm(value)", "state.page='projectSettings'"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("settings routing contains obsolete behavior %q", forbidden)
		}
	}
	for _, marker := range []string{"id=\"syncManagedProject\"", "id=\"editManagedProjectPolicy\"", "id=\"addManagedProjectPrompt\"", "id=\"editManagedProjectBranch\"", "id=\"enableManagedProjectMember\"", "id=\"enableManagedProjectAgent\""} {
		if !strings.Contains(source, marker) {
			t.Errorf("project management drawer is missing %q", marker)
		}
	}
}

func TestLocalAgentWorkflowIsEmbedded(t *testing.T) {
	data, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, marker := range []string{
		"local_codex_cli",
		"maxConcurrentTasks",
		"turnTimeoutMinutes",
		"acceptTags",
		"rejectTags",
		"isAgentTask",
		"pauseAfterPlan",
		"pauseBeforeCompletion",
		"workflowType",
		"simple_conversation",
		"confirm_merge",
		"confirm_close",
		"['created','in_progress','completed','closed']",
	} {
		if !strings.Contains(source, marker) {
			t.Errorf("local Agent workflow is missing %q", marker)
		}
	}
	if strings.Contains(string(mustReadEmbedded(t, "index.html")), "agent-execution.md") {
		t.Error("the embedded application still links the removed Agent execution guide")
	}
}

func TestTaskBoardSupportsStageDragAndDrop(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{"wireTaskBoardDragAndDrop", "dragstart", "dragover", "drop", "pointerdown", "elementFromPoint", "event.stopPropagation()", "targetStage", "/stage"} {
		if !strings.Contains(app, marker) {
			t.Errorf("task board drag-and-drop is missing %q", marker)
		}
	}
	for _, marker := range []string{"[draggable=\"true\"]", ".can-drop", ".drag-over", ".is-dragging"} {
		if !strings.Contains(css, marker) {
			t.Errorf("task board drag-and-drop styling is missing %q", marker)
		}
	}
}

func mustReadEmbedded(t *testing.T, name string) []byte {
	t.Helper()
	data, err := Files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
