//go:build visualparity

package visualparity

import (
	"cmp"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tolerance is the largest per-channel difference counted as equal: the
// same frame rasterized twice is identical, so this only absorbs rounding.
const tolerance = 8

// cell is the grid size, in pixels, that differing pixels are grouped by.
const cell = 8

// TestCompare compares two TestCapture directories state by state and
// writes report.md with diff images. It fails when any state differs.
func TestCompare(t *testing.T) {
	base, head, report := os.Getenv("SAILRY_PARITY_BASE"), os.Getenv("SAILRY_PARITY_HEAD"), os.Getenv("SAILRY_PARITY_REPORT")
	if base == "" || head == "" || report == "" {
		t.Skip("SAILRY_PARITY_BASE, SAILRY_PARITY_HEAD and SAILRY_PARITY_REPORT are not set; run internal/visualparity/run.sh")
	}
	if err := os.MkdirAll(report, 0o755); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, dir := range []string{base, head} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range matches {
			names[strings.TrimSuffix(filepath.Base(match), ".json")] = true
		}
	}
	sorted := slices.Sorted(func(yield func(string) bool) {
		for name := range names {
			if !yield(name) {
				return
			}
		}
	})
	var out strings.Builder
	fmt.Fprintf(&out, "# Visual parity\n\nBase: `%s`\n\nHead: `%s`\n\nPixel tolerance: %d per channel. Geometry tolerance: 0.5 DIP.\n\n", base, head, tolerance)
	fmt.Fprintf(&out, "| State | Base | Head | Differing pixels | Geometry | Menu |\n|---|---|---|---|---|---|\n")
	var details strings.Builder
	differing := 0
	for _, name := range sorted {
		summary, detail, same := compareState(t, base, head, report, name)
		fmt.Fprintf(&out, "| %s | %s |\n", name, summary)
		if !same {
			differing++
			fmt.Fprintf(&details, "\n## %s\n\n%s", name, detail)
		}
	}
	fmt.Fprintf(&out, "\n%d of %d states differ.\n%s", differing, len(sorted), details.String())
	if err := os.WriteFile(filepath.Join(report, "report.md"), []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("report: %s", filepath.Join(report, "report.md"))
	if differing > 0 {
		t.Errorf("%d of %d states differ; see %s", differing, len(sorted), filepath.Join(report, "report.md"))
	}
}

func load(dir, name string) (*record, image.Image) {
	body, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return nil, nil
	}
	var r record
	if json.Unmarshal(body, &r) != nil {
		return nil, nil
	}
	file, err := os.Open(filepath.Join(dir, name+".png"))
	if err != nil {
		return &r, nil
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		return &r, nil
	}
	return &r, picture
}

func status(r *record) string {
	if r == nil {
		return "missing"
	}
	if !r.Stable && r.Status == "captured" {
		return "captured (unsettled)"
	}
	return r.Status
}

func compareState(t *testing.T, baseDir, headDir, reportDir, name string) (string, string, bool) {
	base, baseImage := load(baseDir, name)
	head, headImage := load(headDir, name)
	var detail strings.Builder
	for _, side := range []struct {
		label string
		r     *record
	}{{"base", base}, {"head", head}} {
		if side.r != nil && side.r.Detail != "" {
			fmt.Fprintf(&detail, "- %s note: %s\n", side.label, side.r.Detail)
		}
	}
	if base == nil || head == nil || base.Status != "captured" || head.Status != "captured" {
		return fmt.Sprintf("%s | %s | – | – | –", status(base), status(head)), detail.String() + "- Not compared: a side was not captured.\n", false
	}
	pixels, clusters := "–", ""
	samePixels := true
	if baseImage == nil || headImage == nil {
		samePixels = false
		pixels = "image missing"
	} else if baseImage.Bounds() != headImage.Bounds() {
		samePixels = false
		pixels = fmt.Sprintf("size %v vs %v", baseImage.Bounds().Size(), headImage.Bounds().Size())
	} else {
		count, report := diffImages(t, baseImage, headImage, filepath.Join(reportDir, name+"-diff.png"), base.Scale)
		total := baseImage.Bounds().Dx() * baseImage.Bounds().Dy()
		pixels = fmt.Sprintf("%d (%.3f%%)", count, 100*float64(count)/float64(total))
		samePixels, clusters = count == 0, report
	}
	geometry, moved := compareAccess(base.Access, head.Access)
	texts := compareTexts(base.Texts, head.Texts)
	menu := "same"
	sameMenu := slices.Equal(base.Menu, head.Menu)
	if !sameMenu {
		menu = "differs"
		fmt.Fprintf(&detail, "- Menu: base %q, head %q\n", base.Menu, head.Menu)
	}
	if clusters != "" {
		fmt.Fprintf(&detail, "- Diff image: `%s-diff.png`; regions (DIP x,y w×h, base → head colour at the region's most changed pixel):\n%s", name, clusters)
	}
	if geometry != "" {
		fmt.Fprintf(&detail, "- Accessibility geometry:\n%s", geometry)
	}
	if texts != "" {
		fmt.Fprintf(&detail, "- Text boxes:\n%s", texts)
	}
	if !base.Stable || !head.Stable {
		fmt.Fprintf(&detail, "- Unsettled frame: base stable=%t, head stable=%t; pixel differences may include animation.\n", base.Stable, head.Stable)
	}
	same := samePixels && moved == 0 && sameMenu && texts == ""
	geometryLabel := "same"
	if moved > 0 {
		geometryLabel = fmt.Sprintf("%d changes", moved)
	}
	return fmt.Sprintf("%s | %s | %s | %s | %s", status(base), status(head), pixels, geometryLabel, menu), detail.String(), same
}

func channelDelta(a, b color.Color) int {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := max(abs(int(ar>>8)-int(br>>8)), abs(int(ag>>8)-int(bg>>8)), abs(int(ab>>8)-int(bb>>8)))
	return d
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

type region struct {
	minX, minY, maxX, maxY int
	pixels                 int
	worst                  int
	wx, wy                 int
}

// diffImages writes the base image dimmed with differing pixels in red and
// groups differing cells into connected regions.
func diffImages(t *testing.T, base, head image.Image, path string, scale float32) (int, string) {
	bounds := base.Bounds()
	out := image.NewRGBA(bounds)
	cols, rows := (bounds.Dx()+cell-1)/cell, (bounds.Dy()+cell-1)/cell
	marked := make([]int, cols*rows)
	worst := make([][3]int, cols*rows)
	count := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			a, b := base.At(x, y), head.At(x, y)
			if d := channelDelta(a, b); d > tolerance {
				count++
				out.Set(x, y, color.RGBA{R: 255, A: 255})
				index := (y-bounds.Min.Y)/cell*cols + (x-bounds.Min.X)/cell
				marked[index]++
				if d > worst[index][0] {
					worst[index] = [3]int{d, x, y}
				}
				continue
			}
			r, g, bl, _ := a.RGBA()
			gray := uint8((r>>8 + g>>8 + bl>>8) / 3 / 3)
			out.Set(x, y, color.RGBA{R: gray, G: gray, B: gray, A: 255})
		}
	}
	if count == 0 {
		return 0, ""
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, out); err != nil {
		t.Fatal(err)
	}
	file.Close()
	seen := make([]bool, len(marked))
	var regions []region
	for start := range marked {
		if marked[start] == 0 || seen[start] {
			continue
		}
		r := region{minX: math.MaxInt, minY: math.MaxInt}
		stack := []int{start}
		seen[start] = true
		for len(stack) > 0 {
			index := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cx, cy := index%cols, index/cols
			r.minX, r.minY, r.maxX, r.maxY = min(r.minX, cx), min(r.minY, cy), max(r.maxX, cx), max(r.maxY, cy)
			r.pixels += marked[index]
			if worst[index][0] > r.worst {
				r.worst, r.wx, r.wy = worst[index][0], worst[index][1], worst[index][2]
			}
			for _, step := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := cx+step[0], cy+step[1]
				if nx < 0 || ny < 0 || nx >= cols || ny >= rows {
					continue
				}
				next := ny*cols + nx
				if marked[next] > 0 && !seen[next] {
					seen[next] = true
					stack = append(stack, next)
				}
			}
		}
		regions = append(regions, r)
	}
	slices.SortFunc(regions, func(a, b region) int { return cmp.Compare(b.pixels, a.pixels) })
	var report strings.Builder
	for i, r := range regions {
		if i == 12 {
			fmt.Fprintf(&report, "  - … %d more regions\n", len(regions)-i)
			break
		}
		dip := func(v int) float32 { return float32(v*cell) / scale }
		fmt.Fprintf(&report, "  - %.0f,%.0f %.0f×%.0f: %d px, %s → %s\n", dip(r.minX), dip(r.minY), dip(r.maxX-r.minX+1), dip(r.maxY-r.minY+1),
			r.pixels, hex(base.At(r.wx, r.wy)), hex(head.At(r.wx, r.wy)))
	}
	return count, report.String()
}

func key(n accessNode) string { return n.Role + "\x00" + n.Name }

// compareAccess pairs nodes by role, name and order of appearance.
func compareAccess(base, head []accessNode) (string, int) {
	index := func(nodes []accessNode) (map[string][]accessNode, []string) {
		groups := map[string][]accessNode{}
		var order []string
		for _, n := range nodes {
			k := key(n)
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], n)
		}
		return groups, order
	}
	baseGroups, order := index(base)
	headGroups, headOrder := index(head)
	var out strings.Builder
	changes := 0
	line := func(format string, args ...any) {
		changes++
		if changes <= 40 {
			fmt.Fprintf(&out, "  - "+format+"\n", args...)
		}
	}
	far := func(a, b float32) bool { return math.Abs(float64(a-b)) > 0.5 }
	for _, k := range order {
		before, after := baseGroups[k], headGroups[k]
		for i, b := range before {
			if i >= len(after) {
				line("missing in head: %s %q at %.1f,%.1f %.1f×%.1f", b.Role, b.Name, b.X, b.Y, b.W, b.H)
				continue
			}
			a := after[i]
			if far(a.X, b.X) || far(a.Y, b.Y) || far(a.W, b.W) || far(a.H, b.H) {
				line("%s %q: %.1f,%.1f %.1f×%.1f → %.1f,%.1f %.1f×%.1f", b.Role, b.Name, b.X, b.Y, b.W, b.H, a.X, a.Y, a.W, a.H)
			}
			if a.Disabled != b.Disabled || a.Focused != b.Focused || a.Checked != b.Checked || a.Expanded != b.Expanded || a.Value != b.Value || a.Placeholder != b.Placeholder {
				line("%s %q state: disabled %t→%t focused %t→%t checked %t→%t expanded %t→%t value %q→%q placeholder %q→%q", b.Role, b.Name,
					b.Disabled, a.Disabled, b.Focused, a.Focused, b.Checked, a.Checked, b.Expanded, a.Expanded, b.Value, a.Value, b.Placeholder, a.Placeholder)
			}
		}
	}
	for _, k := range headOrder {
		before, after := baseGroups[k], headGroups[k]
		for i := len(before); i < len(after); i++ {
			a := after[i]
			line("extra in head: %s %q at %.1f,%.1f %.1f×%.1f", a.Role, a.Name, a.X, a.Y, a.W, a.H)
		}
	}
	if changes > 40 {
		fmt.Fprintf(&out, "  - … %d more\n", changes-40)
	}
	return out.String(), changes
}

func compareTexts(base, head []textBox) string {
	headBoxes := map[string]textBox{}
	for _, box := range head {
		headBoxes[box.Text] = box
	}
	baseBoxes := map[string]bool{}
	var out strings.Builder
	lines := 0
	add := func(format string, args ...any) {
		lines++
		if lines <= 30 {
			fmt.Fprintf(&out, "  - "+format+"\n", args...)
		}
	}
	far := func(a, b float32) bool { return math.Abs(float64(a-b)) > 0.5 }
	for _, b := range base {
		baseBoxes[b.Text] = true
		a, ok := headBoxes[b.Text]
		if !ok {
			add("only in base: %q at %.1f,%.1f", b.Text, b.X, b.Y)
		} else if far(a.X, b.X) || far(a.Y, b.Y) || far(a.W, b.W) || far(a.H, b.H) {
			add("%q: %.1f,%.1f %.1f×%.1f → %.1f,%.1f %.1f×%.1f", b.Text, b.X, b.Y, b.W, b.H, a.X, a.Y, a.W, a.H)
		}
	}
	for _, a := range head {
		if !baseBoxes[a.Text] {
			add("only in head: %q at %.1f,%.1f", a.Text, a.X, a.Y)
		}
	}
	if lines > 30 {
		fmt.Fprintf(&out, "  - … %d more\n", lines-30)
	}
	return out.String()
}
