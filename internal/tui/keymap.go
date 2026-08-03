package tui

// action is the abstract intent a keypress maps to on a base screen. Keeping
// key→action translation as a pure function (dispatch) makes the keymap
// unit-testable independently of the model's side effects.
type action int

const (
	actNone       action = iota
	actQuit              // q on the list — clean quit
	actQuitHard          // ctrl+c anywhere
	actBack              // esc — pop one screen (ignored at the root)
	actHelp              // ? — help overlay
	actRefresh           // r — refetch current screen
	actUp                // ↑ / k
	actDown              // ↓ / j
	actTop               // g
	actBottom            // G
	actOpen              // enter — drill in (list→checklist, checklist→note)
	actToggle            // space / x — toggle item completion
	actSearch            // / — enter search mode
	actCopy              // c — copy Markdown to clipboard
	actNewTask           // n
	actEditTitle         // e — edit task title (list) / item title (checklist)
	actEditDesc          // E — edit task description in $EDITOR (list)
	actSetDue            // t — set due date
	actDelete            // d — delete (with confirm)
	actAddItem           // a — add checklist item
	actEditNote          // e — edit item note in $EDITOR (note screen)
	actCycleState        // s — advance item state: none → ready → progress → review → none
	actSetPR             // p — set / clear the item's PR number
	actOpenPR            // o — open the item's PR in a browser
)

// dispatch maps a key (bubbletea String() form) to an action for the given base
// screen. Overlays and the secret/not-logged-in screens are handled elsewhere;
// dispatch only covers the list, checklist, and note screens.
func dispatch(scr screen, key string) action {
	// Global bindings win on every base screen.
	switch key {
	case "ctrl+c":
		return actQuitHard
	case "?":
		return actHelp
	case "r":
		return actRefresh
	}
	switch scr {
	case screenList:
		return listAction(key)
	case screenChecklist:
		return checklistAction(key)
	case screenNote:
		return noteAction(key)
	}
	return actNone
}

func listAction(key string) action {
	switch key {
	case "up", "k":
		return actUp
	case "down", "j":
		return actDown
	case "g":
		return actTop
	case "G":
		return actBottom
	case "enter":
		return actOpen
	case "/":
		return actSearch
	case "q":
		return actQuit
	case "esc":
		return actBack
	case "c":
		return actCopy
	case "n":
		return actNewTask
	case "e":
		return actEditTitle
	case "E":
		return actEditDesc
	case "t":
		return actSetDue
	case "d":
		return actDelete
	}
	return actNone
}

func checklistAction(key string) action {
	switch key {
	case "up", "k":
		return actUp
	case "down", "j":
		return actDown
	case "g":
		return actTop
	case "G":
		return actBottom
	case "space", "x":
		return actToggle
	case "enter":
		return actOpen
	case "esc":
		return actBack
	case "c":
		return actCopy
	case "a":
		return actAddItem
	case "e":
		return actEditTitle
	case "d":
		return actDelete
	case "s":
		return actCycleState
	case "p":
		return actSetPR
	case "o":
		return actOpenPR
	}
	return actNone
}

func noteAction(key string) action {
	switch key {
	case "e":
		return actEditNote
	case "c":
		return actCopy
	case "o":
		return actOpenPR
	case "esc":
		return actBack
	case "up", "k":
		return actUp
	case "down", "j":
		return actDown
	case "g":
		return actTop
	case "G":
		return actBottom
	}
	return actNone
}
