package tui

import (
	"sort"
	"strings"
	"unicode"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// Search matching for `/` on the list and checklist screens. Pure and
// unit-tested; the model only decides which query and which data to feed it.
//
// Two strengths of match, by the length of the text:
//   - Titles are fuzzy: every whitespace-separated query term must appear in
//     order in the title, gaps allowed (fzf-style), and the title is ranked by
//     how tight and word-aligned the hits are.
//   - Descriptions and notes are word matches: every term must appear as a
//     plain substring. A subsequence match over a long body would let almost
//     any short query through, and the filter would stop filtering.
//
// All matching is case-insensitive and rune-indexed, so the positions it hands
// back index straight into []rune(title) for highlighting.

// searchTerms lower-cases the query and splits it on whitespace. nil means no
// query — everything passes.
func searchTerms(query string) [][]rune {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return nil
	}
	out := make([][]rune, len(fields))
	for i, f := range fields {
		out[i] = lowerRunes(f)
	}
	return out
}

// lowerRunes lower-cases rune by rune, so index i of the result is index i of
// the input — strings.ToLower may change the rune count.
func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

// fuzzyTitle matches every term against title as a subsequence. It returns the
// summed score and the rune positions to highlight, or ok=false when a term
// doesn't fit.
func fuzzyTitle(terms [][]rune, title string) (score int, pos []int, ok bool) {
	text := lowerRunes(title)
	for _, term := range terms {
		s, p, hit := fuzzyTerm(term, text)
		if !hit {
			return 0, nil, false
		}
		score += s
		pos = append(pos, p...)
	}
	sort.Ints(pos)
	return score, pos, true
}

// fuzzyTerm finds term in text as a subsequence, preferring the tightest window
// (fzf's v1 scheme: first forward hit, then walk back from its end to the
// latest start) or a contiguous occurrence when that scores higher.
func fuzzyTerm(term, text []rune) (int, []int, bool) {
	if len(term) == 0 {
		return 0, nil, true
	}
	end, ti := -1, 0
	for i, r := range text {
		if r == term[ti] {
			ti++
			if ti == len(term) {
				end = i
				break
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}
	start, ti := 0, len(term)-1
	for i := end; i >= 0; i-- {
		if text[i] == term[ti] {
			ti--
			if ti < 0 {
				start = i
				break
			}
		}
	}
	pos := make([]int, 0, len(term))
	ti = 0
	for i := start; i <= end && ti < len(term); i++ {
		if text[i] == term[ti] {
			pos = append(pos, i)
			ti++
		}
	}
	best, bestPos := fuzzyScore(text, pos), pos
	for _, at := range indexAll(text, term) {
		exact := make([]int, len(term))
		for k := range exact {
			exact[k] = at + k
		}
		if s := fuzzyScore(text, exact) + exactBonus; s > best {
			best, bestPos = s, exact
		}
	}
	return best, bestPos, true
}

// exactBonus tips a tie toward the term typed out whole: "api" should light up
// "api", not the a-p-i of "a pile".
const exactBonus = 15

// fuzzyScore rewards hits that start words and run together, and charges for
// gaps inside the matched window.
func fuzzyScore(text []rune, pos []int) int {
	score := 100 - 2*(pos[len(pos)-1]-pos[0]+1-len(pos))
	for k, p := range pos {
		if p == 0 || !isWordRune(text[p-1]) {
			score += 10
		}
		if k > 0 && pos[k-1] == p-1 {
			score += 8
		}
	}
	return score
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// indexAll returns the start of every occurrence of term in text.
func indexAll(text, term []rune) []int {
	var out []int
	for i := 0; i+len(term) <= len(text); i++ {
		if runesEqualAt(text, term, i) {
			out = append(out, i)
		}
	}
	return out
}

func runesEqualAt(text, term []rune, at int) bool {
	for k, r := range term {
		if text[at+k] != r {
			return false
		}
	}
	return true
}

// wordsMatch reports whether every term occurs in text as a substring — the
// match rule for descriptions and notes.
func wordsMatch(terms [][]rune, text string) bool {
	if len(terms) == 0 || strings.TrimSpace(text) == "" {
		return false
	}
	lower := lowerRunes(text)
	for _, term := range terms {
		if len(indexAll(lower, term)) == 0 {
			return false
		}
	}
	return true
}

// termPositions marks every rune of every occurrence of any term in text, for
// highlighting a note body line by line (a line can hold some terms and not
// others, so this is deliberately "any", not "all").
func termPositions(terms [][]rune, text string) []int {
	lower := lowerRunes(text)
	seen := map[int]bool{}
	var pos []int
	for _, term := range terms {
		for _, at := range indexAll(lower, term) {
			for k := range term {
				if !seen[at+k] {
					seen[at+k] = true
					pos = append(pos, at+k)
				}
			}
		}
	}
	sort.Ints(pos)
	return pos
}

// itemMatch is one checklist item's result: which title runes to highlight and
// whether the note matched (the row's note flag changes colour).
type itemMatch struct {
	item  *client.ChecklistItem
	title []int
	note  bool
	score int // the title's fuzzy score; 0 for a note-only hit
}

// matchItem matches an item's title fuzzily and its note by words. With no
// terms every item passes unmarked.
func matchItem(terms [][]rune, it *client.ChecklistItem) (itemMatch, bool) {
	m := itemMatch{item: it}
	if len(terms) == 0 {
		return m, true
	}
	score, pos, titleHit := fuzzyTitle(terms, it.Title)
	m.title, m.score = pos, score
	m.note = it.Notes != nil && wordsMatch(terms, *it.Notes)
	return m, titleHit || m.note
}

// filterItems keeps the items that match, in checklist order — positions mean
// something on a checklist, so it isn't re-ranked.
func filterItems(terms [][]rune, items []*client.ChecklistItem) []itemMatch {
	out := make([]itemMatch, 0, len(items))
	for _, it := range items {
		if m, ok := matchItem(terms, it); ok {
			out = append(out, m)
		}
	}
	return out
}

// taskMatch is one task's result on the list: highlighted title runes, whether
// the description matched, and the matching items of its checklist when that
// has been loaded.
type taskMatch struct {
	task  *client.Task
	title []int
	desc  bool
	items []itemMatch

	tier  int // 0 title, 1 description, 2 items only — lower ranks first
	score int // the title's score in tier 0, the best item's in tier 2
}

// filterTasks matches the list against terms and ranks it: title hits first,
// then description hits, then tasks reached only through their checklist —
// titles and items each by best score. Ties keep the server's order. full holds the loaded full
// tasks (items + notes) by id; a task missing from it is matched on title and
// description alone. With no terms every task passes unmarked, in order.
func filterTasks(terms [][]rune, tasks []*client.Task, full map[int64]*client.Task) []taskMatch {
	out := make([]taskMatch, 0, len(tasks))
	for _, t := range tasks {
		m := taskMatch{task: t}
		if len(terms) == 0 {
			out = append(out, m)
			continue
		}
		score, pos, titleHit := fuzzyTitle(terms, t.Title)
		m.title, m.score = pos, score
		m.desc = wordsMatch(terms, t.Description)
		if ft := full[t.ID]; ft != nil {
			for _, it := range ft.ChecklistItems {
				if im, ok := matchItem(terms, it); ok {
					m.items = append(m.items, im)
				}
			}
		}
		switch {
		case titleHit:
			m.tier = 0
		case m.desc:
			m.tier = 1
		case len(m.items) > 0:
			m.tier = 2
			m.score = 0
			for _, im := range m.items {
				m.score = max(m.score, im.score)
			}
		default:
			continue
		}
		out = append(out, m)
	}
	if len(terms) > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].tier != out[j].tier {
				return out[i].tier < out[j].tier
			}
			return out[i].tier != 1 && out[i].score > out[j].score
		})
	}
	return out
}
