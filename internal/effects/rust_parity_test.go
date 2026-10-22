package effects

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in oracle uses the actual checked-out upstream Rust functions, not a
// handwritten approximation in another language. No Rust dependency is shipped.
func TestRustSourceParity(t *testing.T) {
	if os.Getenv("KEYNOPE_RUST_PARITY") != "1" {
		t.Skip("set KEYNOPE_RUST_PARITY=1 with local Craft reference checkouts")
	}
	function := func(path, name string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "attic", path))
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		start := strings.Index(s, "fn "+name+"(")
		if start < 0 {
			t.Fatal(name)
		}
		brace := strings.Index(s[start:], "{") + start
		depth := 0
		for i := brace; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return s[start : i+1]
				}
			}
		}
		t.Fatal("unfinished Rust function", name)
		return ""
	}
	source := `#![allow(dead_code)]
#[derive(Clone,Copy)] enum GradientStyle {Linear,Radial,Angle,Reflected,Diamond}
#[derive(Clone,Copy)] struct Rect{x0:i32,y0:i32,x1:i32,y1:i32}
impl Rect {fn width(self)->i32{self.x1-self.x0} fn height(self)->i32{self.y1-self.y0}}
enum StrokePosition {Inside,Center,Outside}
struct StrokeFx{position:StrokePosition,size:f32}
`
	source += function("effectcraft/crates/raster/src/blur.rs", "box_radii") + "\n"
	source += function("photocraft/crates/compose/src/effects.rs", "gradient_units") + "\n"
	source += function("photocraft/crates/compose/src/effects.rs", "gradient_t") + "\n"
	source += strings.ReplaceAll(function("photocraft/crates/compose/src/effects.rs", "stroke_widths"), "photocraft_doc::", "") + "\n"
	source += `fn main(){
for sigma in [0.0,0.1,0.5,1.0,1.5,2.0,4.0,10.0,24.0] {let r=box_radii(sigma,3);println!("b {} {} {} {}",sigma,r[0],r[1],r[2]);}
for (name,style) in [("radial",GradientStyle::Radial),("reflected",GradientStyle::Reflected),("diamond",GradientStyle::Diamond),("angle",GradientStyle::Angle)] {
 for (w,h) in [(200,100),(100,200),(160,100)] {for y in [0.5,h as f32/2.0,h as f32-0.5] {for x in [0.5,w as f32/2.0,w as f32-0.5] {
 let t=gradient_t(style,0.0,1.0,false,(0.0,0.0),Rect{x0:0,y0:0,x1:w,y1:h},x,y);println!("g {} {} {} {} {} {:.9}",name,w,h,x,y,t);
 }}}
}
for level in [2.0f32,3.0,6.0,32.0] {let lv=level.round().clamp(2.0,255.0)-1.0;for i in 0..=1024 {let v=i as f32/1024.0;println!("p {} {} {:.9}",level,v,(v.clamp(0.0,1.0)*lv).round()/lv);}}
for (name,position) in [("inside",StrokePosition::Inside),("center",StrokePosition::Center),("outside",StrokePosition::Outside)]{let (inside,outside)=stroke_widths(&StrokeFx{position,size:7.5});println!("s {} {} {}",name,inside,outside);}
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "oracle.rs")
	binary := filepath.Join(dir, "oracle")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := exec.Command("rustc", "--edition=2021", "-O", path, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("rustc: %v\n%s", err, data)
	}
	output, err := exec.Command(binary).Output()
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		rows++
		switch line[0] {
		case 's':
			var position string
			var inside, outside float64
			fmt.Sscanf(line, "s %s %f %f", &position, &inside, &outside)
			a, b := StrokeWidths(position, 7.5)
			if a != inside || b != outside {
				t.Fatal(line, a, b)
			}
		case 'b':
			var sigma float64
			var a, b, c int
			fmt.Sscanf(line, "b %f %d %d %d", &sigma, &a, &b, &c)
			got := BoxRadii(sigma)
			if sigma <= .05 {
				got = []int{0, 0, 0}
			}
			if len(got) != 3 || got[0] != a || got[1] != b || got[2] != c {
				t.Fatal(line, got)
			}
		case 'g':
			var style string
			var w, h, x, y, want float64
			fmt.Sscanf(line, "g %s %f %f %f %f %f", &style, &w, &h, &x, &y, &want)
			got := GradientT(style, w, h, x, y)
			if math.Abs(got-want) > 2e-6 {
				t.Fatal(line, got)
			}
		case 'p':
			var level int
			var v, want float64
			fmt.Sscanf(line, "p %d %f %f", &level, &v, &want)
			table := PosterizeTable(level)
			got := table[min(len(table)-1, int(v*float64(len(table))))]
			if math.Abs(got-want) > 1e-6 {
				t.Fatal(line, got)
			}
		}
	}
	t.Logf("%d comparisons against compiled upstream Rust functions passed", rows)
}
