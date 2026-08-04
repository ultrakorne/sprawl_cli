package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/icons"
)

// newChecklistModel opens the checklist screen on a loaded task.
func newChecklistModel(fc *fakeClient, task *client.Task) *Model {
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "sek", SecretProvided: true,
		NewClient: func(string, string) Client { return fc },
	})
	m.width, m.height = 100, 30
	m.Update(tasksLoadedMsg{tasks: []*client.Task{task}, validated: true})
	m.push(screenChecklist)
	m.detail = task
	m.pendingTaskID = task.ID
	return m
}

func demoTask() *client.Task {
	return &client.Task{
		ID: 3, Title: "Ship the API",
		Project:           &client.Project{ID: 1, Name: "Sprawl", Key: "sprawl", GithubURL: "https://github.com/ultrakorne/sprawl"},
		ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 2},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 7, Title: "add the migration", Position: 1},
			{ID: 8, Title: "write the tests", Position: 2},
		},
	}
}

// -- pure helpers -----------------------------------------------------------

func TestNextState_Cycles(t *testing.T) {
	want := []string{client.StateReadyToPickup, client.StateInProgress, client.StateInReview, ""}
	cur := ""
	for i, w := range want {
		cur = nextState(cur)
		if cur != w {
			t.Fatalf("step %d = %q, want %q", i, cur, w)
		}
	}
	// An unknown value (a state added server-side later) restarts rather than
	// sticking, so the key never becomes a no-op.
	if got := nextState("blocked"); got != client.StateReadyToPickup {
		t.Fatalf("unknown state → %q", got)
	}
}

func TestStateIconsAreOneCell(t *testing.T) {
	// The per-glyph width invariant is owned by internal/icons; what this side
	// has to guarantee is that the TUI's own column constant agrees with it.
	// Widen the glyphs without widening stateColW and every row carrying one
	// shears.
	if stateColW != icons.Width {
		t.Fatalf("stateColW = %d but icons.Width = %d", stateColW, icons.Width)
	}
	for _, set := range []icons.Set{icons.Nerd, icons.Plain} {
		for _, glyph := range []string{set.Ready, set.Progress, set.Review, icons.Unknown} {
			if w := lipgloss.Width(glyph); w != stateColW {
				t.Errorf("glyph %q width = %d, want %d", glyph, w, stateColW)
			}
		}
	}
}

func TestStateIconAndNames(t *testing.T) {
	s := newStyles()
	if s.stateIcon("") != "" || s.stateBadge("") != "" {
		t.Fatal("a stateless item must render no icon and no badge")
	}
	for state, name := range map[string]string{
		client.StateReadyToPickup: "Ready to pick up",
		client.StateInProgress:    "In progress",
		client.StateInReview:      "In review",
	} {
		if s.stateIcon(state) == "" {
			t.Errorf("no icon for %q", state)
		}
		if stateName(state) != name {
			t.Errorf("name for %q = %q, want %q", state, stateName(state), name)
		}
		// The badge is icon + full name — what the item header shows.
		if got := s.stateBadge(state); got != s.stateIcon(state)+" "+name {
			t.Errorf("badge for %q = %q", state, got)
		}
	}
	for state, word := range map[string]string{
		client.StateReadyToPickup: "ready",
		client.StateInProgress:    "progress",
		client.StateInReview:      "review",
	} {
		if stateLabel(state) != word {
			t.Errorf("label for %q = %q, want %q", state, stateLabel(state), word)
		}
	}
	// A state this build doesn't know still shows a marker and its raw name.
	if s.stateIcon("blocked") == "" || stateName("blocked") != "blocked" {
		t.Fatalf("unknown state rendered as nothing: %q / %q", s.stateIcon("blocked"), stateName("blocked"))
	}
}

// The opt-out itself is tested in internal/icons; what matters here is that the
// TUI actually reads it when it builds its styles, rather than baking in a set.
func TestNewStyles_HonoursIconOptOut(t *testing.T) {
	t.Setenv("SPRAWL_ICONS", "plain")
	if newStyles().icons != icons.Plain {
		t.Fatal("SPRAWL_ICONS=plain should select the fallback set")
	}
	t.Setenv("SPRAWL_ICONS", "")
	if newStyles().icons != icons.Nerd {
		t.Fatal("nerd icons are the default")
	}
}

// -- rendering --------------------------------------------------------------

func TestChecklistRows_ShowIconAndPRColumns(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)

	view := ansi.Strip(m.viewChecklist())
	// The row carries the ICON, never the word — the name is spelled out in the
	// item header instead.
	for _, want := range []string{m.styles.stateIcon(client.StateInReview), "#412", "add the migration", "s state", "o open pr"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "In review") || strings.Contains(view, "review ") {
		t.Errorf("rows must not spell the state out:\n%s", view)
	}
}

// titleCol is the DISPLAY column a row's title starts at. Byte offsets are the
// wrong measure here: the `›` cursor is three bytes and the state glyphs four,
// so a byte index reports a shift where the terminal shows none (and vice
// versa).
func titleCol(t *testing.T, row, title string) int {
	t.Helper()
	plain := ansi.Strip(row)
	i := strings.Index(plain, title)
	if i < 0 {
		t.Fatalf("title %q not in row %q", title, plain)
	}
	return lipgloss.Width(plain[:i])
}

// The whole point of the fixed columns: a row's title sits at the same column
// whether or not the item has a state or a PR, so toggling either doesn't make
// the list jump sideways.
func TestChecklistRows_ColumnsDoNotShift(t *testing.T) {
	task := demoTask()
	m := newChecklistModel(&fakeClient{}, task)
	before := m.checklistRows(task.ChecklistItems, m.bodyHeight(hintsFor(screenChecklist)))
	wantCol := titleCol(t, before[0], "add the migration")
	wantNeighbour := titleCol(t, before[1], "write the tests")

	// Same rows, now with a state and a PR on the first item.
	task.ChecklistItems[0].State = client.StateInProgress
	task.ChecklistItems[0].PRNumber = 412
	after := m.checklistRows(task.ChecklistItems, m.bodyHeight(hintsFor(screenChecklist)))

	if got := titleCol(t, after[0], "add the migration"); got != wantCol {
		t.Errorf("title column moved %d → %d when a state+PR were set:\n%q\n%q",
			wantCol, got, ansi.Strip(before[0]), ansi.Strip(after[0]))
	}
	if got := titleCol(t, after[1], "write the tests"); got != wantNeighbour {
		t.Errorf("neighbour row shifted %d → %d", wantNeighbour, got)
	}
}

// A PR number wider than the reserved minimum widens the column for every row
// at once — never just its own.
func TestChecklistRows_WidePRKeepsRowsAligned(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 1234567
	m := newChecklistModel(&fakeClient{}, task)
	rows := m.checklistRows(task.ChecklistItems, m.bodyHeight(hintsFor(screenChecklist)))
	a := titleCol(t, rows[0], "add the migration")
	b := titleCol(t, rows[1], "write the tests")
	if a != b {
		t.Errorf("rows misaligned at columns %d vs %d\n%q\n%q", a, b, ansi.Strip(rows[0]), ansi.Strip(rows[1]))
	}
}

// The item header is where the state gets its full name, per the web app's
// pill labels — plus the PR number.
func TestNoteHeader_SpellsOutStateAndPR(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInProgress
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)
	m.push(screenNote)

	view := ansi.Strip(m.viewNote())
	for _, want := range []string{"add the migration", "In progress", m.styles.icons.Progress, "#412"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in header:\n%s", want, view)
		}
	}
}

// -- s: cycle state ---------------------------------------------------------

func TestCycleState_SendsNextStateAndUpdatesLocally(t *testing.T) {
	fc := &fakeClient{}
	m := newChecklistModel(fc, demoTask())

	cmd := m.press("s")
	if got := m.detail.ChecklistItems[0].State; got != client.StateReadyToPickup {
		t.Fatalf("optimistic state = %q", got)
	}
	runCmd(cmd)
	if len(fc.calls) != 1 || fc.calls[0] != "SetState:7" {
		t.Fatalf("calls = %v", fc.calls)
	}
	if fc.stateAttrs["state"] != client.StateReadyToPickup {
		t.Fatalf("attrs = %v", fc.stateAttrs)
	}
	// Only the state key travels — the PR number must not be disturbed.
	if _, present := fc.stateAttrs["pr_number"]; present {
		t.Fatalf("attrs leaked pr_number: %v", fc.stateAttrs)
	}

	// A second press advances rather than re-sending the same value, because the
	// local copy moved with the first one.
	runCmd(m.press("s"))
	if fc.stateAttrs["state"] != client.StateInProgress {
		t.Fatalf("second press attrs = %v", fc.stateAttrs)
	}
}

func TestCycleState_ClearSendsExplicitNull(t *testing.T) {
	fc := &fakeClient{}
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	m := newChecklistModel(fc, task)

	runCmd(m.press("s"))
	v, present := fc.stateAttrs["state"]
	if !present || v != nil {
		t.Fatalf("clearing must send a present null, got %v", fc.stateAttrs)
	}
	if m.detail.ChecklistItems[0].State != "" {
		t.Fatalf("local state = %q", m.detail.ChecklistItems[0].State)
	}
}

// Setting a state un-completes the item server-side; the optimistic copy has to
// mirror that or the progress counter jumps when the response lands.
func TestCycleState_UncompletesItemLocally(t *testing.T) {
	fc := &fakeClient{}
	task := demoTask()
	task.ChecklistItems[0].Completed = true
	task.ChecklistProgress = client.ChecklistProgress{Done: 1, Total: 2}
	m := newChecklistModel(fc, task)

	m.press("s")
	if m.detail.ChecklistItems[0].Completed {
		t.Fatal("setting a state must un-complete the item")
	}
	if m.detail.ChecklistProgress.Done != 0 {
		t.Fatalf("progress = %d/%d", m.detail.ChecklistProgress.Done, m.detail.ChecklistProgress.Total)
	}
}

func TestItemStateSetMsg_AppliesServerCopy(t *testing.T) {
	m := newChecklistModel(&fakeClient{}, demoTask())
	notes := "keep me"
	m.detail.ChecklistItems[0].Notes = &notes

	m.Update(itemStateSetMsg{
		item:  &client.ChecklistItem{ID: 7, Title: "add the migration", State: client.StateInReview, PRNumber: 412},
		label: "#7 → review",
	})

	it := m.detail.ChecklistItems[0]
	if it.State != client.StateInReview || it.PRNumber != 412 {
		t.Fatalf("item = %+v", it)
	}
	// The state response carries no notes; the loaded body must survive so the
	// note screen doesn't go blank.
	if it.Notes == nil || *it.Notes != "keep me" {
		t.Fatalf("notes = %v", it.Notes)
	}
	if !strings.Contains(m.status, "#7 → review") {
		t.Fatalf("status = %q", m.status)
	}
}

// A failed write resyncs from the server rather than guessing a rollback:
// state and completion interact, so the server's copy is the only truth.
func TestItemStateFailed_ResyncsFromServer(t *testing.T) {
	fc := &fakeClient{}
	m := newChecklistModel(fc, demoTask())

	_, cmd := m.Update(itemStateFailedMsg{err: errors.New("boom"), context: "set state"})
	if !m.statusErr || !strings.Contains(m.status, "boom") {
		t.Fatalf("status = %q err=%v", m.status, m.statusErr)
	}
	runCmd(cmd)
	if len(fc.calls) != 1 || fc.calls[0] != "GetTask:3" {
		t.Fatalf("calls = %v", fc.calls)
	}
}

// -- p: set PR number -------------------------------------------------------

func TestSetPR_PromptsAndWrites(t *testing.T) {
	fc := &fakeClient{}
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 100
	m := newChecklistModel(fc, task)

	m.press("p")
	if m.overlay != ovInput || m.inputKind != inSetPR {
		t.Fatalf("overlay = %d kind = %d", m.overlay, m.inputKind)
	}
	// Prefilled with the current number so a correction doesn't mean retyping.
	if m.input.String() != "100" {
		t.Fatalf("prefill = %q", m.input.String())
	}

	m.input.setValue("412")
	cmd := m.press("enter")
	runCmd(cmd)
	if len(fc.calls) != 1 || fc.calls[0] != "SetState:7" {
		t.Fatalf("calls = %v", fc.calls)
	}
	if fc.stateAttrs["pr_number"] != int64(412) {
		t.Fatalf("attrs = %v", fc.stateAttrs)
	}
	// The state key must be absent — a PR write leaves the state alone.
	if _, present := fc.stateAttrs["state"]; present {
		t.Fatalf("attrs leaked state: %v", fc.stateAttrs)
	}
}

func TestSetPR_EmptyClears(t *testing.T) {
	fc := &fakeClient{}
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(fc, task)

	m.press("p")
	m.input.reset()
	runCmd(m.press("enter"))

	v, present := fc.stateAttrs["pr_number"]
	if !present || v != nil {
		t.Fatalf("clear must send a present null: %v", fc.stateAttrs)
	}
}

func TestSetPR_RejectsNonPositiveLocally(t *testing.T) {
	fc := &fakeClient{}
	m := newChecklistModel(fc, demoTask())

	for _, bad := range []string{"abc", "0", "-4"} {
		m.press("p")
		m.input.setValue(bad)
		runCmd(m.press("enter"))
		if len(fc.calls) != 0 {
			t.Fatalf("%q reached the server: %v", bad, fc.calls)
		}
		if !m.statusErr {
			t.Fatalf("%q should surface an error", bad)
		}
	}
}

// -- completing clears the state --------------------------------------------

// Completing an item clears its state server-side, so the icon has to go the
// moment the box is ticked — not when something later refetches the task.
func TestToggle_ClearsStateOptimistically(t *testing.T) {
	fc := &fakeClient{toggleResp: &client.ChecklistItem{ID: 7, Title: "add the migration", Completed: true}}
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(fc, task)

	cmd := m.press("x")
	it := m.detail.ChecklistItems[0]
	if !it.Completed {
		t.Fatal("item should be completed optimistically")
	}
	if it.State != "" {
		t.Errorf("state should clear with the tick, got %q", it.State)
	}
	// The PR number is independent and survives completion.
	if it.PRNumber != 412 {
		t.Errorf("PR number cleared: %d", it.PRNumber)
	}
	runCmd(cmd)
}

// The toggle response is authoritative about state too — copying only
// `completed` out of it left a stale icon on screen.
func TestItemToggled_TakesServerState(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	m := newChecklistModel(&fakeClient{}, task)
	notes := "keep me"
	m.detail.ChecklistItems[0].Notes = &notes

	m.Update(itemToggledMsg{item: &client.ChecklistItem{
		ID: 7, Title: "add the migration", Completed: true, State: "", PRNumber: 412,
	}})

	it := m.detail.ChecklistItems[0]
	if it.State != "" || !it.Completed || it.PRNumber != 412 {
		t.Fatalf("server copy not applied: %+v", it)
	}
	if it.Notes == nil || *it.Notes != "keep me" {
		t.Errorf("notes lost: %v", it.Notes)
	}
	if m.detail.ChecklistProgress.Done != 1 {
		t.Errorf("progress not recomputed: %+v", m.detail.ChecklistProgress)
	}
}

// A rejected toggle puts BOTH the completion flag and the state back.
func TestToggleFailed_RestoresStateToo(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	m := newChecklistModel(&fakeClient{}, task)

	m.press("x") // optimistic: completed + state cleared
	m.Update(toggleFailedMsg{itemID: 7, prev: false, prevState: client.StateInReview, err: errors.New("nope")})

	it := m.detail.ChecklistItems[0]
	if it.Completed || it.State != client.StateInReview {
		t.Fatalf("revert incomplete: completed=%v state=%q", it.Completed, it.State)
	}
}

// -- o: open the PR ---------------------------------------------------------

func TestOpenPR_ResolvesTheLink(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)

	// The command is the opener itself; assert on the URL it was handed by
	// resolving the same way the handler does.
	if got := client.PRURL(m.detail.Project, 412); got != "https://github.com/ultrakorne/sprawl/pull/412" {
		t.Fatalf("resolved = %q", got)
	}
	if _, cmd := m.openPRLink(); cmd == nil {
		t.Fatal("o on an item with a resolvable PR should produce a command")
	}
}

// Both ways the link can fail to resolve are ordinary states: say so on the
// footer and do nothing, rather than erroring.
func TestOpenPR_MissingPieces(t *testing.T) {
	t.Run("no pr number", func(t *testing.T) {
		m := newChecklistModel(&fakeClient{}, demoTask())
		m.openPRLink()
		if !strings.Contains(m.status, "no PR number") || m.statusErr {
			t.Fatalf("status = %q err=%v", m.status, m.statusErr)
		}
	})
	t.Run("no repo url", func(t *testing.T) {
		task := demoTask()
		task.Project = &client.Project{ID: 1, Name: "Sprawl"} // no github_url
		task.ChecklistItems[0].PRNumber = 412
		m := newChecklistModel(&fakeClient{}, task)
		m.openPRLink()
		if !strings.Contains(m.status, "no GitHub URL") || m.statusErr {
			t.Fatalf("status = %q err=%v", m.status, m.statusErr)
		}
	})
}

// PR numbers are OSC 8 hyperlinks so a click opens them, without the TUI
// capturing the mouse (which would break the terminal's own text selection).
func TestPRHyperlink(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)
	url := "https://github.com/ultrakorne/sprawl/pull/412"

	linked := m.linkPR("PR #412", 412)
	if !strings.Contains(linked, url) {
		t.Fatalf("no link target in %q", linked)
	}
	// The escapes must be invisible to both the width maths and the plain-text
	// view, or every column that follows would shear.
	if got := ansi.Strip(linked); got != "PR #412" {
		t.Errorf("visible text = %q", got)
	}
	if got := lipgloss.Width(linked); got != len("PR #412") {
		t.Errorf("width = %d, want %d", got, len("PR #412"))
	}

	// Clipping a line that contains a link must leave the sequence balanced,
	// otherwise the link would bleed into the rest of the screen.
	long := "note · item #7 " + strings.Repeat("x", 60) + "  ·  " + linked
	for _, w := range []int{80, 40, 20, 5} {
		clipped := m.clip(long, w)
		if opens := strings.Count(clipped, "\x1b]8;;"); opens != 2 {
			t.Errorf("w=%d: %d OSC 8 markers, want 2 (balanced): %q", w, opens, clipped)
		}
	}

	// Nothing to point at → plain text, never a dangling escape.
	task.Project = &client.Project{ID: 1, Name: "Sprawl"} // no github_url
	if got := m.linkPR("PR #412", 412); got != "PR #412" {
		t.Errorf("unresolvable link should stay plain, got %q", got)
	}
}

// prField is the rendered form — styled AND linked. The two have to compose in
// one specific order (link outside, style inside): lipgloss renders an OSC 8
// escape in its input as literal text, so the wrong order prints the URL.
func TestPRField_StyledAndLinked(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)

	linked := m.prField("PR #412", 412, false)
	if got := ansi.Strip(linked); got != "PR #412" {
		t.Fatalf("visible text = %q — the URL leaked into the output", got)
	}
	if got := lipgloss.Width(linked); got != len("PR #412") {
		t.Errorf("width = %d, want %d", got, len("PR #412"))
	}
	if !strings.Contains(linked, "https://github.com/ultrakorne/sprawl/pull/412") {
		t.Error("no link target")
	}
	if !strings.Contains(linked, "\x1b[") {
		t.Error("no styling — a link the user can't see isn't discoverable")
	}

	// An unresolvable PR must NOT get the link colour: the styling is the
	// promise that clicking does something.
	task.Project = &client.Project{ID: 1, Name: "Sprawl"} // no github_url
	inert := m.prField("PR #412", 412, false)
	if strings.Contains(inert, "\x1b]8;;") {
		t.Error("unresolvable PR must not be hyperlinked")
	}
	if inert == m.styles.link.Render("PR #412") {
		t.Error("unresolvable PR must not wear the link colour")
	}
}

// The underline is what marks a PR as clickable, so it has to survive the row
// being selected — a link that stops looking like one when the cursor lands on
// it reads as a rendering glitch.
func TestPRField_KeepsUnderlineWhenSelected(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].PRNumber = 412
	m := newChecklistModel(&fakeClient{}, task)

	const underline = "4" // SGR 4
	sel := m.prField("#412", 412, true)
	if !strings.Contains(sel, "\x1b[") || !strings.Contains(sel, underline) {
		t.Errorf("selected PR lost its underline: %q", sel)
	}
	if !strings.Contains(sel, "\x1b]8;;") {
		t.Errorf("selected PR lost its hyperlink: %q", sel)
	}
	if got := ansi.Strip(sel); got != "#412" {
		t.Errorf("visible text = %q", got)
	}

	// And it's actually rendered that way in the selected row.
	rows := m.checklistRows(task.ChecklistItems, m.bodyHeight(hintsFor(screenChecklist)))
	if !strings.Contains(rows[0], "\x1b]8;;") {
		t.Errorf("selected row lost the link: %q", rows[0])
	}
}

// Nothing in any rendered screen may show a raw OSC 8 payload. This is the
// regression guard for the whole class of bug: anything that re-renders a
// string already carrying a hyperlink turns the URL into visible text.
func TestScreens_NeverLeakLinkEscapes(t *testing.T) {
	task := demoTask()
	task.ChecklistItems[0].State = client.StateInReview
	task.ChecklistItems[0].PRNumber = 412
	task.ChecklistItems[1].PRNumber = 77 // an unselected row carries one too
	m := newChecklistModel(&fakeClient{}, task)

	views := map[string]string{"checklist": m.viewChecklist()}
	m.push(screenNote)
	views["note"] = m.viewNote()
	m.itemSel = 1 // the linked row is now the UNSELECTED one
	m.popTo(screenChecklist)
	views["checklist/other-selection"] = m.viewChecklist()

	for name, view := range views {
		plain := ansi.Strip(view)
		if strings.Contains(plain, "]8;;") || strings.Contains(plain, "https://") {
			t.Errorf("%s leaks a link escape into visible text:\n%s", name, plain)
		}
	}
}

func TestOpenURL_RefusesNonHTTPS(t *testing.T) {
	// The only thing standing between a malformed github_url and the platform's
	// URL handler.
	for _, u := range []string{"file:///etc/passwd", "http://example.com", "javascript:alert(1)", ""} {
		if err := openURL(u); !errors.Is(err, errNotBrowsable) {
			t.Errorf("openURL(%q) = %v, want refusal", u, err)
		}
	}
}

func TestURLOpenFailed_FallsBackToClipboard(t *testing.T) {
	m := newChecklistModel(&fakeClient{}, demoTask())
	url := "https://github.com/ultrakorne/sprawl/pull/412"

	_, cmd := m.Update(urlOpenFailedMsg{url: url, err: errors.New("exec: xdg-open: not found")})
	if got := clipboardPayload(t, cmd); got != url {
		t.Fatalf("clipboard = %q, want the URL", got)
	}
	if !strings.Contains(m.status, url) {
		t.Fatalf("status should name the copied URL: %q", m.status)
	}
}

// -- footer -----------------------------------------------------------------

// Hints take one row when they fit and only spill onto a second when they
// don't — a short screen shouldn't spend a line it doesn't need.
func TestHintLines_OnlyWrapsWhenFull(t *testing.T) {
	hints := "a one · b two · c three"
	if got := hintLines(hints, 80); len(got) != 1 || got[0] != hints {
		t.Fatalf("fits on one line, got %q", got)
	}
	// Narrow enough to need two, and the break lands on a separator so no
	// binding is cut in half.
	const w = 16
	got := hintLines(hints, w)
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(got), got)
	}
	if strings.Join(got, hintSep) != hints {
		t.Errorf("rows lost or reordered bindings: %q", got)
	}
	for _, row := range got {
		if lipgloss.Width(row) > w {
			t.Errorf("row overflows: %q", row)
		}
	}
	// The BOTTOM row is the one that gets filled: the last binding still on the
	// top row must not fit on the bottom one, or it should have been down there.
	top, bottom := got[0], got[1]
	segs := strings.Split(top, hintSep)
	spillover := segs[len(segs)-1] + hintSep + bottom
	if lipgloss.Width(spillover) <= w {
		t.Errorf("bottom row isn't full — %q still fits below %q", segs[len(segs)-1], bottom)
	}
	// Never more than the cap: a very narrow window keeps the tail on the last
	// row (frame clips it) rather than dropping bindings.
	if got := hintLines(hints, 4); len(got) > maxHintRows {
		t.Errorf("want at most %d rows, got %d: %q", maxHintRows, len(got), got)
	}
	if hintLines("", 80) != nil {
		t.Error("no hints should mean no rows")
	}
}

// Every screen fills the window exactly, and the body grows by a row on the
// screens whose hints fit on one line.
func TestFrame_HeightMatchesHintRows(t *testing.T) {
	m := newChecklistModel(&fakeClient{}, demoTask())
	m.width, m.height = 100, 24

	for name, view := range map[string]string{
		"list":      m.viewList(),
		"checklist": m.viewChecklist(),
		"note":      m.viewNote(),
		"help":      m.frame("Help", helpLines(m.styles), "any key to close"),
	} {
		if lines := strings.Split(view, "\n"); len(lines) != m.height {
			t.Errorf("%s: %d lines, want %d", name, len(lines), m.height)
		}
	}

	// At 100 columns the note screen's hints fit on one line and the checklist's
	// don't, so the note body is one row taller. That difference is the point.
	noteH := m.bodyHeight(hintsFor(screenNote))
	checklistH := m.bodyHeight(hintsFor(screenChecklist))
	if got := len(hintLines(hintsFor(screenNote), 100)); got != 1 {
		t.Fatalf("note hints should fit one row at w=100, got %d", got)
	}
	if got := len(hintLines(hintsFor(screenChecklist), 100)); got != 2 {
		t.Fatalf("checklist hints should need two rows at w=100, got %d", got)
	}
	if noteH != checklistH+1 {
		t.Errorf("note body = %d, checklist body = %d; want the note one row taller", noteH, checklistH)
	}
	if noteH != m.height-fixedChrome-1 {
		t.Errorf("bodyHeight = %d, want %d", noteH, m.height-fixedChrome-1)
	}
}

// -- clipboard --------------------------------------------------------------

func TestItemMarkdown_CarriesStateCommentAndPRLink(t *testing.T) {
	task := demoTask()
	it := task.ChecklistItems[0]
	it.State = client.StateInReview
	it.PRNumber = 412

	md := itemMarkdown(it, task)
	// The trailing comment matches the server's export format byte for byte, so
	// a pasted file round-trips both fields.
	if !strings.Contains(md, "<!-- state: in_review pr: 412 -->") {
		t.Fatalf("markdown = %q", md)
	}
	if !strings.Contains(md, "pr: https://github.com/ultrakorne/sprawl/pull/412") {
		t.Fatalf("markdown should resolve the link: %q", md)
	}
}

func TestTaskMarkdown_UnchangedWithoutStateOrPR(t *testing.T) {
	task := demoTask()
	task.Project = &client.Project{ID: 1, Name: "Sprawl"} // no repo
	md := taskMarkdown(task)
	if strings.Contains(md, "<!--") {
		t.Fatalf("stateless items must carry no comment: %q", md)
	}
	if strings.Contains(md, "repo:") {
		t.Fatalf("a project without a repo must not emit a repo line: %q", md)
	}
}

func TestTaskMarkdown_CarriesRepo(t *testing.T) {
	md := taskMarkdown(demoTask())
	if !strings.Contains(md, "repo: https://github.com/ultrakorne/sprawl") {
		t.Fatalf("markdown = %q", md)
	}
}
