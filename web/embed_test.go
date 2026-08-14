package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestKnowledgeEditorExposesAgentDiscoveryMetadata(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	for _, marker := range []string{`name="summary"`, `name="triggerDescription"`, "knowledgeSearchText(node)", "nodeSummaryNote", "nodeTriggerNote"} {
		if !strings.Contains(app, marker) {
			t.Errorf("knowledge discovery metadata UI is missing %q", marker)
		}
	}
}

func TestOverriddenTaskRendererUsesMutableBinding(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	if strings.Contains(app, "const renderTask=") && strings.Count(app, "renderTask=") > 1 {
		t.Fatal("renderTask is declared const but later renderer layers reassign it")
	}
}

func TestStylesheetsHaveBalancedBlocks(t *testing.T) {
	for _, name := range []string{"assets/styles.css", "assets/management-drawers.css", "assets/reference-theme.css"} {
		source := string(mustReadEmbedded(t, name))
		opens, closes := strings.Count(source, "{"), strings.Count(source, "}")
		if opens != closes {
			t.Errorf("%s has %d opening braces and %d closing braces", name, opens, closes)
		}
	}
}

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

func TestQueueAndTaskUseSharedPageStructure(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"app-page-head", "app-page-tabs", "app-page-toolbar", "app-page-content", "work-main", "task-workflow-track", "task-conversation", "task-preview", "child-list"} {
		if !strings.Contains(string(app), marker) {
			t.Errorf("task UI is missing shared structure %q", marker)
		}
		if !strings.Contains(string(css), "."+marker) {
			t.Errorf("task UI is missing shared styling for %q", marker)
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
	for _, marker := range []string{"id=\"syncManagedProject\"", "id=\"editManagedProjectPolicy\"", "id=\"addManagedProjectPrompt\"", "id=\"editManagedProjectBranch\"", "id=\"enableManagedProjectMember\""} {
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

func TestActiveTaskConversationLocalizesSystemEvents(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	workflowCapture := strings.Index(app, "const localWorkflowUI=")
	if workflowCapture < 0 {
		t.Fatal("could not locate the active local workflow capture")
	}
	rendererStart := strings.LastIndex(app[:workflowCapture], "renderTask=async function")
	if rendererStart < 0 {
		t.Fatal("could not locate the active task renderer")
	}
	renderer := app[rendererStart:workflowCapture]
	if strings.Contains(renderer, "JSON.stringify(p)") {
		t.Error("active task renderer exposes raw event payload JSON in the conversation")
	}
	if !strings.Contains(renderer, "localizedConversationEvent(e,p)") || !strings.Contains(renderer, "localizedConversationAuthor(e,actors,p)") {
		t.Error("active task renderer does not pass non-message events through localization")
	}
	for _, marker := range []string{"taskCreatedEvent:'任务已创建，当前阶段为“创建”'", "agentStartedEvent:'本地 Codex Agent 已开始执行任务。'", "agent_restart_queued:'agentRestartQueuedEvent'", "agentRestartQueuedEvent:'ProjectBoard 服务已重启，本地 Agent 已自动排队继续执行。'"} {
		if !strings.Contains(app, marker) {
			t.Errorf("conversation localization is missing %q", marker)
		}
	}
}

func TestRunningAgentIsNamedOnTaskSurfaces(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	workflowCapture := strings.Index(app, "const localWorkflowUI=")
	if workflowCapture < 0 {
		t.Fatal("could not locate the active local workflow capture")
	}
	activeWorkflow := app[:workflowCapture]
	queueStart := strings.LastIndex(activeWorkflow, "renderQueue=async function")
	taskStart := strings.LastIndex(activeWorkflow, "renderTask=async function")
	if queueStart < 0 || taskStart < 0 {
		t.Fatal("could not locate active queue and task renderers")
	}
	queueRenderer := activeWorkflow[queueStart:taskStart]
	taskRenderer := activeWorkflow[taskStart:]
	for name, source := range map[string]string{"queue": queueRenderer, "task": taskRenderer} {
		if !strings.Contains(source, "agentRunningMarkup") {
			t.Errorf("%s renderer does not name the Agent that is currently executing", name)
		}
	}
	if !strings.Contains(activeWorkflow, "t('agentRunningLabel',{agent:agentName})") {
		t.Error("shared running Agent renderer does not include the localized Agent name")
	}
}

func TestRunningAgentShowsCurrentInvocationElapsedTime(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{
		"function activeAgentStartedAt",
		"execution.state==='running'",
		"function formatAgentElapsed",
		"data-agent-started-at",
		"setInterval(update,1000)",
		"agentElapsed:'已执行 {duration}'",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("running Agent elapsed-time UI is missing %q", marker)
		}
	}
	for _, marker := range []string{".agent-running-copy", ".task-agent-duration", "font-variant-numeric: tabular-nums"} {
		if !strings.Contains(css, marker) {
			t.Errorf("running Agent elapsed-time styling is missing %q", marker)
		}
	}
}

func TestActiveTaskPageKeepsPrototypeBreadcrumbBackButton(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	start := strings.Index(app, "localWorkflowUI.renderTask=async function")
	end := strings.Index(app[start:], "function pendingFilesMarkup")
	if start < 0 || end < 0 {
		t.Fatal("could not locate the active task enhancement wrapper")
	}
	wrapper := app[start : start+end]
	if strings.Contains(wrapper, "redundantBack.remove()") {
		t.Error("task enhancement wrapper removes the exported breadcrumb back button")
	}
	if !strings.Contains(app, `id="back" type="button" title="${t('backToQueue')}"`) || !strings.Contains(app, "$('#back')?.addEventListener('click'") {
		t.Error("active task page does not expose a working breadcrumb back button")
	}
}

func TestTaskDetailPollsForConversationAndStateUpdates(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	for _, marker := range []string{
		"function taskLiveSignature",
		"function scheduleTaskLiveRefresh",
		"item.conversation||[]",
		"latestSignature!==baselineSignature",
		"draft?.value",
		"drawer?.classList.contains('open')",
		"scheduleTaskLiveRefresh(item)",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("task detail live refresh is missing %q", marker)
		}
	}
}

func TestTaskDetailUsesConversationCenteredThreeColumnWorkspace(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	index := string(mustReadEmbedded(t, "index.html"))
	for _, marker := range []string{
		"task-focus-page",
		"task-focus-top",
		"task-focus-top-actions",
		"task-focus-nav",
		"task-focus-conversation",
		"task-focus-inspector",
		"task-chat-event",
		"task-inspector-progress",
		"task-detail-shell",
		"taskNavigationGroups",
		"taskWorkspaceIDs('pinned')",
		"rememberTaskVisit(item.id)",
		"pinnedTasks",
		"attentionTasks",
		"followedTasks",
		"recentTasks",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("three-column task workspace is missing %q", marker)
		}
	}
	for _, marker := range []string{
		"grid-template-columns: var(--task-nav-width) minmax(390px, 1fr) var(--task-inspector-width)",
		"grid-template-rows: 56px minmax(0, 1fr)",
		"--task-nav-width: 282px",
		"--task-inspector-width: 334px",
		".workspace:has(.task-focus-page)",
		".task-nav-pin.active",
		".task-focus-conversation .chat-composer",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("three-column task workspace styling is missing %q", marker)
		}
	}
	for _, marker := range []string{
		`$('.task-focus-context')?.remove()`,
		`class="task-inspector-description"`,
		`taskDescriptionSection:'任务描述'`,
		`${t('taskDescriptionSection')}`,
		`opacity: .62`,
	} {
		if !strings.Contains(app+css, marker) {
			t.Errorf("streamlined task navigation is missing %q", marker)
		}
	}
	if !strings.Contains(index, `id="i-pin"`) {
		t.Error("task navigation is missing its Lucide pin symbol")
	}
	for _, removed := range []string{`<span class="eyebrow">Attention map</span>`, `id="taskNavSearch"`, `class="task-focus-sync"`} {
		if strings.Contains(app, removed) {
			t.Errorf("task detail still renders removed chrome %q", removed)
		}
	}
}

func TestAllTabsShareTheSameApplicationSidebar(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{
		"navigation(group)",
		"this.navigation('primary')",
		"this.navigation('secondary')",
		"this.navigation('utility')",
		"class=\"project-switch\" id=\"projectSwitch\"",
		"class=\"nav rail-primary\"",
		"class=\"nav rail-bottom\"",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("shared application sidebar is missing %q", marker)
		}
	}
	for _, pageSpecificRule := range []string{
		".shell.task-detail-shell .mast",
		".shell.task-detail-shell .rail",
		".shell.task-detail-shell .project-switch",
		".shell.task-detail-shell .rail-primary",
		".shell.task-detail-shell .rail-bottom",
		".shell.task-detail-shell .nav",
		".shell.task-detail-shell .workspace",
	} {
		if strings.Contains(css, pageSpecificRule) {
			t.Errorf("task detail still overrides the shared application sidebar: %q", pageSpecificRule)
		}
	}
}

func TestAllTabsUseTheSharedPageFramework(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	for _, marker := range []string{
		"const pageAdapters=[",
		"const pageFramework={",
		"open(pageID,context={})",
		"route()",
		"hydrate()",
		"async render()",
		"shell()",
		"currentRouteHash=()=>pageFramework.route()",
		"applyRouteFromURL=()=>pageFramework.hydrate()",
		"navigate=pageID=>pageFramework.open(pageID)",
		"renderPage=()=>pageFramework.render()",
		"renderShell=()=>pageFramework.shell()",
		"id:'queue'",
		"const taskWorkspacePage={",
		"id:'taskWorkspace'",
		"id:'knowledge'",
		"id:'agentRequests'",
		"id:'messages'",
		"id:'settings'",
		"taskWorkspacePage.open(item.id)",
		"taskWorkspacePage.open(row.dataset.workItem)",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("shared page framework is missing %q", marker)
		}
	}
	for _, obsolete := range []string{"const knowledgePageRouter=renderPage", "const knowledgeShell=renderShell", "const knowledgeBaseRoute=currentRouteHash"} {
		if strings.Contains(app, obsolete) {
			t.Errorf("page-specific framework wrapper still exists: %q", obsolete)
		}
	}
	if strings.LastIndex(app, "boot();") < strings.Index(app, "const pageFramework={") {
		t.Error("application boots before the shared page framework is installed")
	}
}

func TestTaskBoardFiltersByInclusiveCreatedDateRange(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{
		"function taskCreatedDateKey",
		"id=\"queueDateFrom\"",
		"id=\"queueDateTo\"",
		"createdDate>=state.queueDateFrom",
		"createdDate<=state.queueDateTo",
		"id=\"clearQueueDates\"",
		"dateClearButton.hidden=!state.queueDateFrom&&!state.queueDateTo",
		"dateFrom.value>dateTo.value",
		"dateTo.value<dateFrom.value",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("task board created-date range filter is missing %q", marker)
		}
	}
	for _, marker := range []string{".queue-date-filter", ".queue-date-clear", ".queue-date-clear[hidden]", ":focus-within"} {
		if !strings.Contains(css, marker) {
			t.Errorf("task board created-date range styling is missing %q", marker)
		}
	}
}

func TestTaskBoardFiltersShareOneControlSystem(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	index := string(mustReadEmbedded(t, "index.html"))
	for _, marker := range []string{
		"class=\"queue-filter-bar\"",
		"data-filter-field=\"query\"",
		"data-filter-field=\"tag\"",
		"data-filter-field=\"priority\"",
		"data-filter-field=\"blocker\"",
		"data-filter-field=\"date\"",
		"id=\"clearQueueFilters\"",
		"syncFilterChrome",
		"clearFiltersButton.onclick",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("unified task filter bar is missing %q", marker)
		}
	}
	for _, marker := range []string{".queue-filter-bar", ".queue-filter-field", ".queue-filter-body", ".queue-filter-icon", ".queue-filter-clear", ".queue-filter-field.is-active"} {
		if !strings.Contains(css, marker) {
			t.Errorf("unified task filter styling is missing %q", marker)
		}
	}
	if !strings.Contains(index, "id=\"i-calendar\"") {
		t.Error("task date filter is missing its Lucide calendar symbol")
	}
}

func TestSettingsTabsFillAvailableWorkspace(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{
		"wrapper.dataset.settingsSection=state.settingsSection",
		"settings-reference-content global-settings-reference",
	} {
		if !strings.Contains(app, marker) {
			t.Errorf("settings tab workspace is missing %q", marker)
		}
	}
	for _, marker := range []string{
		".settings-reference-content.global-settings-reference",
		"grid-template-rows: auto auto minmax(0, 1fr)",
		".global-settings-reference > .settings-page-shell",
		"[data-settings-section=\"account\"]",
		"[data-settings-section=\"system\"]",
		".settings-reference-content > .settings-management-content",
		".settings-management-content > .settings-management-card",
		"scrollbar-gutter: stable",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("settings tab fill layout is missing %q", marker)
		}
	}
}

func TestPageTitlesUseCompactTopRhythm(t *testing.T) {
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{
		"padding-top: clamp(22px, 2.4vw, 32px)",
		"margin-top: 14px",
		"margin-bottom: 20px",
		"margin-top: 10px",
		"margin-bottom: 16px",
	} {
		if !strings.Contains(css, marker) {
			t.Errorf("compact page-title rhythm is missing %q", marker)
		}
	}
}

func TestSidebarCollapseAndLocaleControlsUseRequestedPositions(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))
	css := string(mustReadEmbedded(t, "assets/reference-theme.css"))
	for _, marker := range []string{"mastActions=$('.mast-actions')", "collapse.className='rail-toggle'", "collapse.innerHTML=icon('chevron')", "mastActions.append(collapse)"} {
		if !strings.Contains(app, marker) {
			t.Errorf("sidebar toggle is missing mast control behavior %q", marker)
		}
	}
	for _, marker := range []string{"locale.classList.add('nav-locale')", "locale.innerHTML=`${icon('globe')}", "bottom.insertBefore(locale,settingsButton||null)"} {
		if !strings.Contains(app, marker) {
			t.Errorf("locale switch is missing sidebar-tab behavior %q", marker)
		}
	}
	if strings.Contains(app, "rail.insertBefore(controls") || strings.Contains(app, "shell.append(collapse)") {
		t.Error("sidebar toggle still uses an obsolete placement")
	}
	for _, marker := range []string{".rail-toggle {", ".nav-locale .icon", ".shell.rail-collapsed .mast .brand { display: none; }", ".shell.rail-collapsed .mast-actions", ".mast-actions .rail-toggle { display: none; }"} {
		if !strings.Contains(css, marker) {
			t.Errorf("sidebar shell control styling is missing %q", marker)
		}
	}
}

func TestFinalUIOnlyExposesSupportedLocalAgentWorkflow(t *testing.T) {
	app := string(mustReadEmbedded(t, "assets/app.js"))

	if !strings.Contains(app, "runtimeAgentID=item.assigned_agent_id") {
		t.Error("task assignee rendering ignores the Agent selected by the local executor")
	}

	configurationStart := strings.LastIndex(app, "taskConfigurationMarkup=function")
	configurationEnd := strings.Index(app[configurationStart:], "wireTaskConfiguration=function")
	if configurationStart < 0 || configurationEnd < 0 {
		t.Fatal("could not locate the final task configuration renderer")
	}
	configuration := app[configurationStart : configurationStart+configurationEnd]
	if strings.Contains(configuration, "configAbandonTask") {
		t.Error("task configuration exposes abandonment even though the four-state API has no abandon transition")
	}

	policyStart := strings.LastIndex(app, "function managedProjectPolicy")
	policyEnd := strings.Index(app[policyStart:], "function managedProjectPrompt")
	if policyStart < 0 || policyEnd < 0 {
		t.Fatal("could not locate the final project policy editor")
	}
	policy := app[policyStart : policyStart+policyEnd]
	for _, unsupported := range []string{"allowAgentExecution", "allowAgentAutoClose", "allowAgentAutoCloseSubtasks"} {
		if strings.Contains(policy, unsupported) {
			t.Errorf("project policy editor exposes unsupported setting %q", unsupported)
		}
	}

	projectSettingsStart := strings.LastIndex(app, "projectSettings=async function")
	projectSettingsEnd := strings.Index(app[projectSettingsStart:], "Object.assign(messages.zh,")
	if projectSettingsStart < 0 || projectSettingsEnd < 0 {
		t.Fatal("could not locate the final project settings drawer")
	}
	projectSettings := app[projectSettingsStart : projectSettingsStart+projectSettingsEnd]
	for _, unsupported := range []string{"['agents','agents']", "enableManagedProjectAgent", "data-disable-managed-agent"} {
		if strings.Contains(projectSettings, unsupported) {
			t.Errorf("project settings exposes organization-level Agent control as project-level behavior: %q", unsupported)
		}
	}

	for _, obsoleteCopy := range []string{
		"配置公开地址、固定 Agent SSH 身份",
		"SSH 公钥与活动连接会立即失效",
		"pinned Agent SSH identity",
		"SSH keys and active connections are revoked immediately",
	} {
		if strings.Contains(app[strings.LastIndex(app, "Object.assign(messages.zh,"):], obsoleteCopy) {
			t.Errorf("final translation overrides retain obsolete SSH copy %q", obsoleteCopy)
		}
	}
	finalTranslations := app
	for _, currentCopy := range []string{"本地 Codex 执行身份、并发容量和运行状态", "HTTP 与 SQLite WAL 均可用", "local Codex execution identities, capacity, and runtime status", "HTTP and SQLite WAL are available"} {
		if !strings.Contains(finalTranslations, currentCopy) {
			t.Errorf("final translation overrides are missing local-executor copy %q", currentCopy)
		}
	}

	systemStart := strings.LastIndex(app, "renderSystemSettings=async function")
	systemEnd := strings.Index(app[systemStart:], "Object.assign(messages.zh,")
	if systemStart < 0 || systemEnd < 0 {
		t.Fatal("could not locate the final system settings renderer")
	}
	systemSettings := app[systemStart : systemStart+systemEnd]
	for _, obsolete := range []string{"/api/system/ssh", "Agent SSH", "hostKeyFingerprint"} {
		if strings.Contains(systemSettings, obsolete) {
			t.Errorf("system settings exposes obsolete SSH runtime behavior %q", obsolete)
		}
	}

	agentManagementStart := strings.LastIndex(app, "renderAgentManagement=async function")
	if agentManagementStart < 0 {
		t.Fatal("could not locate the final Agent management renderer")
	}
	agentManagementEnd := strings.Index(app[agentManagementStart:], "agentRequestDetail=async function")
	if agentManagementEnd < 0 {
		agentManagementEnd = len(app) - agentManagementStart
	}
	agentManagement := app[agentManagementStart : agentManagementStart+agentManagementEnd]
	if strings.Contains(agentManagement, "disableForProject") {
		t.Error("organization Agent toggle is labelled as a project-scoped action")
	}
}

func TestTaskWorkflowUsesSharedProjectPageStructure(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/reference-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	appSource := string(app)
	cssSource := string(css)
	for _, marker := range []string{
		"task-workflow-track",
		"task-conversation",
		"task-inspector",
		"task-composer-box",
		"task-drawer-body",
		"task-drawer-footer",
		"stage-evidence-list",
		"project-prompt-form",
		"project-action-form",
	} {
		if !strings.Contains(appSource, marker) {
			t.Errorf("task workflow is missing structure %q", marker)
		}
		if !strings.Contains(cssSource, "."+marker) {
			t.Errorf("task workflow is missing styling for %q", marker)
		}
	}
	for _, marker := range []string{
		`class="page-head task-detail-page-head"`,
		`class="content task-detail-page-content"`,
		`header(t('projectSettings')`,
		`project-settings-page`,
	} {
		if !strings.Contains(appSource, marker) {
			t.Errorf("task workflow is missing shared project page structure %q", marker)
		}
	}
	for _, legacy := range []string{
		`workflowContextChrome('task'`,
		`workflowContextChrome('settings'`,
		`class="task-detail-shell"`,
		`className='project-settings-shell'`,
	} {
		if strings.Contains(appSource, legacy) {
			t.Errorf("task workflow still renders prototype-only structure %q", legacy)
		}
	}
	for _, copy := range []string{"executionContext", "projectSettingsDescription"} {
		if !strings.Contains(appSource, copy) {
			t.Errorf("task workflow is missing required copy %q", copy)
		}
	}
	if strings.Contains(appSource, "$('.context-message')?.remove()") {
		t.Error("task rendering still removes the task description after rendering")
	}
	if strings.Contains(cssSource, ".task-workflow-track { display: none; }") {
		t.Error("the six-stage workflow must remain reachable on narrow screens")
	}
}

func TestAgentManagementSupportsStatusFilterAndPagination(t *testing.T) {
	app, err := Files.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := Files.ReadFile("assets/management-drawers.css")
	if err != nil {
		t.Fatal(err)
	}

	appSource := string(app)
	agentManagementStart := strings.LastIndex(appSource, "renderAgentManagement=async function")
	if agentManagementStart < 0 {
		t.Fatal("could not locate the final Agent management renderer")
	}
	agentManagementEnd := strings.Index(appSource[agentManagementStart:], "agentRequestDetail=async function")
	if agentManagementEnd < 0 {
		agentManagementEnd = len(appSource) - agentManagementStart
	}
	agentManagement := appSource[agentManagementStart : agentManagementStart+agentManagementEnd]
	for _, marker := range []string{"agentStatusFilter", "agentManagementPage", "agentManagementPageSize", "previousAgents", "nextAgents", "noAgentsForStatus"} {
		if !strings.Contains(agentManagement, marker) {
			t.Errorf("Agent management is missing filtering or pagination marker %q", marker)
		}
	}
	for _, marker := range []string{".agent-ops-toolbar", ".agent-ops-pagination"} {
		if !strings.Contains(string(css), marker) {
			t.Errorf("Agent management is missing styling for %q", marker)
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
