package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func whoamiFixture() *client.Whoami {
	return &client.Whoami{
		Agent:     client.Agent{ID: 1, Name: "owner", IsOwner: true},
		Workspace: &client.Workspace{ID: 1, Name: "Default", Role: "owner", Level: "write_create"},
		Workspaces: []client.Workspace{
			{ID: 1, Name: "Default", Role: "owner", Level: "write_create"},
			{ID: 3, Name: "Work_2", Role: "owner", Level: "none"},
			{ID: 9, Name: "Shared", Role: "read", Level: "read"},
		},
	}
}

// workspaceModel starts a secret-authed session and drains the validating
// fetch plus the silent whoami that follows it. Every client the model builds
// is recorded so a switch can be asserted on the selector it was given.
func workspaceModel(t *testing.T, fc *fakeClient, workspace string) (*Model, *[]string) {
	t.Helper()
	shortTransients(t)
	var built []string
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "sek", SecretProvided: true, Workspace: workspace,
		NewClient: func(_, key, ws string) Client {
			built = append(built, key+"|"+ws)
			return fc
		},
	})
	m.width, m.height = 100, 30
	drain(m, m.Init())
	return m, &built
}

// drain runs a command and feeds every message (and every follow-up command's
// messages) back into the model until nothing is left — except the transient
// status clear, which is dropped so a test can still read the footer. The
// timer behind it is shortened (see shortTransients) so draining it is cheap.
func drain(m *Model, cmd tea.Cmd) {
	for _, msg := range runCmd(cmd) {
		if msg == nil {
			continue
		}
		if _, isClear := msg.(clearStatusMsg); isClear {
			continue
		}
		drain(m, m.send(msg))
	}
}

// shortTransients makes setTransient's clear timer fire at once for the test,
// so drain doesn't block ~2.5s on every footer message.
func shortTransients(t *testing.T) {
	t.Helper()
	prev := transientTTL
	transientTTL = time.Millisecond
	t.Cleanup(func() { transientTTL = prev })
}

// The header names the workspace once whoami has answered, breadcrumb-style
// between the app name and the view.
func TestWorkspace_HeaderNamesCurrentWorkspace(t *testing.T) {
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m, built := workspaceModel(t, fc, "")
	if m.current() != screenList {
		t.Fatalf("screen = %v", m.current())
	}
	got := ansi.Strip(m.viewList())
	if !strings.Contains(got, "sprawl · Default · tasks") {
		t.Fatalf("header should name the default workspace:\n%s", got)
	}
	if (*built)[0] != "|" {
		t.Fatalf("initial client should carry no selector, got %q", (*built)[0])
	}
}

// A --workspace selection is resolved against the reachable list, not taken
// from whoami's (default) workspace block.
func TestWorkspace_SelectionResolvedFromList(t *testing.T) {
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m, built := workspaceModel(t, fc, "9")
	if (*built)[0] != "|9" {
		t.Fatalf("initial client should carry the selector, got %q", (*built)[0])
	}
	if m.wsCurrent == nil || m.wsCurrent.ID != 9 {
		t.Fatalf("wsCurrent = %+v, want #9", m.wsCurrent)
	}
	if got := ansi.Strip(m.viewList()); !strings.Contains(got, "sprawl · Shared · tasks") {
		t.Fatalf("header:\n%s", got)
	}
}

// A silent whoami failure leaves the header unnamed and is not an error.
func TestWorkspace_HeaderUnchangedWhenWhoamiFails(t *testing.T) {
	fc := &fakeClient{whoamiErr: &client.APIError{Status: 500}}
	m, _ := workspaceModel(t, fc, "")
	got := ansi.Strip(m.viewList())
	if !strings.Contains(got, "sprawl · tasks") || m.statusErr {
		t.Fatalf("header should be the plain one, no error:\n%s\nstatus=%q", got, m.status)
	}
}

// `w` refetches whoami, opens the picker on the current workspace, and enter
// re-pins the session: a new client with the chosen selector, a fresh list,
// nothing from the old workspace kept.
func TestWorkspace_PickerSwitches(t *testing.T) {
	fc := &fakeClient{
		whoamiResp: whoamiFixture(),
		listResp:   []*client.Task{{ID: 42, Title: "old"}},
		getResp:    &client.Task{ID: 42, Title: "old"},
	}
	m, built := workspaceModel(t, fc, "")
	// Drill into a task so there's detail state to discard.
	drain(m, m.press("enter"))
	if m.current() != screenChecklist {
		t.Fatalf("expected the checklist, got %v", m.current())
	}
	m.pop()

	drain(m, m.press("w"))
	if m.overlay != ovPicker || m.pickerKind != pickWorkspace {
		t.Fatalf("w should open the workspace picker, overlay=%v kind=%v", m.overlay, m.pickerKind)
	}
	if m.pickerSel != 0 || !strings.Contains(m.pickerItems[0].label, "(current)") {
		t.Fatalf("cursor should sit on the current workspace: sel=%d items=%+v", m.pickerSel, m.pickerItems)
	}
	if !strings.Contains(m.pickerItems[1].label, "none") {
		t.Fatalf("a workspace the key can't reach should show its level: %q", m.pickerItems[1].label)
	}

	fc.listResp = []*client.Task{{ID: 7, Title: "new"}}
	drain(m, m.press("down"))
	drain(m, m.press("down"))
	drain(m, m.press("enter"))

	if m.overlay != ovNone || m.current() != screenList {
		t.Fatalf("after the switch: overlay=%v screen=%v", m.overlay, m.current())
	}
	if m.workspace != "9" || m.wsCurrent == nil || m.wsCurrent.ID != 9 {
		t.Fatalf("selection not applied: workspace=%q current=%+v", m.workspace, m.wsCurrent)
	}
	if last := (*built)[len(*built)-1]; last != "|9" {
		t.Fatalf("switch should build a client with the new selector, got %q", last)
	}
	if m.detail != nil || len(m.tasks) != 1 || m.tasks[0].ID != 7 {
		t.Fatalf("old data should be gone and the new list loaded: detail=%v tasks=%+v", m.detail, m.tasks)
	}
	if !strings.Contains(m.status, "switched to Shared") {
		t.Fatalf("status = %q", m.status)
	}
	if got := ansi.Strip(m.viewList()); !strings.Contains(got, "sprawl · Shared · tasks") {
		t.Fatalf("header:\n%s", got)
	}
}

// Choosing the workspace the session is already in is a no-op.
func TestWorkspace_PickingCurrentIsNoop(t *testing.T) {
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m, built := workspaceModel(t, fc, "")
	n := len(*built)
	drain(m, m.press("w"))
	drain(m, m.press("enter"))
	if len(*built) != n || m.workspace != "" || m.overlay != ovNone {
		t.Fatalf("no client should be rebuilt: built=%v workspace=%q overlay=%v", *built, m.workspace, m.overlay)
	}
}

// A key capped at `none` in the target still switches — the server allows
// it — but the footer says why the list will be empty.
func TestWorkspace_SwitchToLevelNoneWarns(t *testing.T) {
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m, _ := workspaceModel(t, fc, "")
	drain(m, m.press("w"))
	drain(m, m.press("down"))
	drain(m, m.press("enter"))
	if m.workspace != "3" || !strings.Contains(m.status, "no access there") {
		t.Fatalf("workspace=%q status=%q", m.workspace, m.status)
	}
}

// A project key pins the workspace server-side (workspace_mismatch otherwise),
// so `w` explains instead of offering a choice that can only fail.
func TestWorkspace_ProjectKeyPinsIt(t *testing.T) {
	shortTransients(t)
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m := projectKeyModel(fc, "acme")
	drain(m, m.Init())
	drain(m, m.press("w"))
	if m.overlay != ovNone || !strings.Contains(m.status, "pins the workspace") {
		t.Fatalf("overlay=%v status=%q", m.overlay, m.status)
	}
	if got := ansi.Strip(m.viewList()); !strings.Contains(got, "sprawl · Default · tasks · acme") {
		t.Fatalf("header should carry both the workspace and the key:\n%s", got)
	}
}

// A pre-workspaces server answers whoami without the picture: `w` says so.
func TestWorkspace_ServerWithoutWorkspaces(t *testing.T) {
	fc := &fakeClient{whoamiResp: &client.Whoami{Agent: client.Agent{ID: 1}}}
	m, _ := workspaceModel(t, fc, "")
	drain(m, m.press("w"))
	if m.overlay != ovNone || !strings.Contains(m.status, "doesn't report workspaces") {
		t.Fatalf("overlay=%v status=%q", m.overlay, m.status)
	}
}

// After a credentials-prompt round trip the selector survives: the prompt
// rebuilds the client with whatever workspace was chosen.
func TestWorkspace_SurvivesCredentialsPrompt(t *testing.T) {
	var built []string
	fc := &fakeClient{listErr: &client.APIError{Status: 401}}
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "bad", SecretProvided: true, Workspace: "3",
		NewClient: func(_, key, ws string) Client {
			built = append(built, key+"|"+ws)
			return fc
		},
	})
	m.width, m.height = 100, 30
	drain(m, m.Init())
	if m.current() != screenCreds {
		t.Fatalf("expected the prompt, got %v", m.current())
	}
	fc.listErr = nil
	fc.whoamiResp = whoamiFixture()
	for _, k := range []string{"g", "o", "o", "d"} {
		m.press(k)
	}
	drain(m, m.press("enter"))
	if last := built[len(built)-1]; last != "|3" {
		t.Fatalf("re-validated client lost the selector: %v", built)
	}
	if m.wsCurrent == nil || m.wsCurrent.ID != 3 {
		t.Fatalf("wsCurrent = %+v", m.wsCurrent)
	}
}

// clientHandle is a fresh identity over a shared fake: two handles compare
// unequal even though they answer from the same fixture.
type clientHandle struct{ *fakeClient }

func TestResolveWorkspace_Pure(t *testing.T) {
	w := whoamiFixture()
	if got := resolveWorkspace(w, ""); got == nil || got.ID != 1 {
		t.Fatalf("default: %+v", got)
	}
	if got := resolveWorkspace(w, "9"); got == nil || got.ID != 9 {
		t.Fatalf("selected: %+v", got)
	}
	if got := resolveWorkspace(w, "77"); got != nil {
		t.Fatalf("unreachable should be nil, got %+v", got)
	}
}

// A workspace-coded 403 on the validating fetch is not a credentials problem
// and the prompt couldn't fix it (no workspace field): it stays a footer
// error that names the selector, and the session keeps its list screen.
func TestWorkspace_MismatchAtStartupIsNotACredentialsFailure(t *testing.T) {
	shortTransients(t)
	fc := &fakeClient{listErr: &client.APIError{Status: 403, Code: "workspace_mismatch"}}
	m := newModel(context.Background(), Deps{
		LoggedIn: true, ProjectKey: "acme", Workspace: "3",
		NewClient: func(string, string, string) Client { return fc },
	})
	m.width, m.height = 100, 30
	drain(m, m.Init())
	if m.current() != screenList {
		t.Fatalf("screen = %v, want the list (not the credentials prompt)", m.current())
	}
	if !m.statusErr || !strings.Contains(m.status, "unset --workspace") {
		t.Fatalf("expected a footer error naming the selector, got err=%v status=%q", m.statusErr, m.status)
	}
	if m.loading {
		t.Fatal("loading should be cleared after the failed fetch")
	}
}

func TestWorkspace_RequiredMidSessionIsAFooterError(t *testing.T) {
	shortTransients(t)
	fc := &fakeClient{whoamiResp: whoamiFixture()}
	m, _ := workspaceModel(t, fc, "")
	fc.listErr = &client.APIError{Status: 403, Code: "workspace_required"}
	drain(m, m.press("r"))
	if m.current() != screenList || !strings.Contains(m.status, "pass --workspace") {
		t.Fatalf("screen=%v status=%q", m.current(), m.status)
	}
}

// A list answer issued before a switch belongs to the old workspace; landing
// after the switch it must not overwrite the new one's list.
func TestWorkspace_StaleListDroppedAfterSwitch(t *testing.T) {
	shortTransients(t)
	fc := &fakeClient{whoamiResp: whoamiFixture(), listResp: []*client.Task{{ID: 1, Title: "old"}}}
	// Every NewClient call hands out a distinct identity over the same fake, the
	// way the real closure builds a fresh *client.Client per switch.
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "sek", SecretProvided: true,
		NewClient: func(string, string, string) Client { return &clientHandle{fc} },
	})
	m.width, m.height = 100, 30
	drain(m, m.Init())
	old := m.client
	// Switch to #9 without draining the refetch, then deliver an old-client answer.
	drain(m, m.press("w"))
	drain(m, m.press("down"))
	drain(m, m.press("down"))
	_, cmd := m.Update(kp("enter"))
	if m.workspace != "9" {
		t.Fatalf("switch didn't apply: %q", m.workspace)
	}
	m.send(tasksLoadedMsg{tasks: []*client.Task{{ID: 42, Title: "stale"}}, from: old})
	if len(m.tasks) != 0 {
		t.Fatalf("stale list applied: %+v", m.tasks)
	}
	m.send(searchResultMsg{tasks: []*client.Task{{ID: 43}}, query: "x", from: old})
	if len(m.tasks) != 0 || m.searchLabel != "" {
		t.Fatalf("stale search applied: %+v %q", m.tasks, m.searchLabel)
	}
	m.send(whoamiLoadedMsg{who: &client.Whoami{Workspace: &client.Workspace{ID: 1, Name: "Default"}}, from: old, openPicker: true})
	if m.overlay != ovNone || m.wsCurrent.ID != 9 {
		t.Fatalf("stale whoami applied: overlay=%v current=%+v", m.overlay, m.wsCurrent)
	}
	// The real refetch (same fake, new client identity is the same fake here) still lands.
	fc.listResp = []*client.Task{{ID: 7, Title: "new"}}
	drain(m, cmd)
	if len(m.tasks) != 1 || m.tasks[0].ID != 7 {
		t.Fatalf("fresh list not applied: %+v", m.tasks)
	}
}

// `w` then `enter` (drill in) before whoami answers: the picker must not
// open over the checklist, and the task fetch keeps its own loading state.
func TestWorkspace_PickerNotOpenedOverChecklist(t *testing.T) {
	fc := &fakeClient{
		whoamiResp: whoamiFixture(),
		listResp:   []*client.Task{{ID: 42, Title: "t"}},
		getResp:    &client.Task{ID: 42, Title: "t"},
	}
	m, _ := workspaceModel(t, fc, "")
	_, whoamiCmdPending := m.Update(kp("w")) // not drained: whoami still in flight
	_, getCmd := m.Update(kp("enter"))
	if m.current() != screenChecklist {
		t.Fatalf("expected the checklist, got %v", m.current())
	}
	for _, msg := range runCmd(whoamiCmdPending) {
		m.send(msg)
	}
	if m.overlay != ovNone {
		t.Fatalf("picker opened over the checklist")
	}
	if !m.loading {
		t.Fatal("the task fetch's loading flag was cleared by the whoami answer")
	}
	for _, msg := range runCmd(getCmd) {
		m.send(msg)
	}
	if m.loading || m.detail == nil {
		t.Fatalf("task fetch should have landed: loading=%v detail=%v", m.loading, m.detail)
	}
}

func TestWorkspace_UnnamedFallback(t *testing.T) {
	w := whoamiFixture()
	w.Workspace.Name = ""
	w.Workspaces[0].Name = ""
	fc := &fakeClient{whoamiResp: w}
	m, _ := workspaceModel(t, fc, "")
	if got := ansi.Strip(m.viewList()); !strings.Contains(got, "sprawl · (unnamed) · tasks") {
		t.Fatalf("header:\n%s", got)
	}
	if items := workspacePickerItems(w.Workspaces, w.Workspace); !strings.HasPrefix(items[0].label, "(unnamed)  #1") {
		t.Fatalf("picker label = %q", items[0].label)
	}
}
