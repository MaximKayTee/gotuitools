package termimg

import (
	"image"
	"image/color"
	"testing"
)

func TestLRU(t *testing.T) {
	c := NewLRU[string, int](2)
	var gone []string
	c.Evicted = func(k string, _ int) { gone = append(gone, k) }
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a")
	c.Put("c", 3)
	if _, ok := c.Get("b"); ok || len(gone) != 1 || gone[0] != "b" || c.Len() != 2 {
		t.Errorf("evicted %v", gone)
	}
	c.Remove("a")
	if _, ok := c.Get("a"); ok {
		t.Error("Remove")
	}
}

func TestIDs(t *testing.T) {
	ids := IDs{Lo: 5, Hi: 8}
	a, b, c := ids.Take(), ids.Take(), ids.Take()
	if a != 5 || b != 6 || c != 7 || ids.Take() != 0 {
		t.Errorf("%d %d %d", a, b, c)
	}
	ids.Free(6)
	if ids.Take() != 6 {
		t.Error("reuse")
	}
}

func TestCircleAndCover(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for i := range src.Pix {
		src.Pix[i] = 0xff
	}
	c := Circle(src)
	if c.Bounds().Dx() != 20 || c.At(0, 0).(color.NRGBA).A != 0 || c.At(10, 10).(color.NRGBA).A != 0xff {
		t.Error("circle")
	}
	if r := Cover(1920, 1080, 1, 1); r != image.Rect(420, 0, 1500, 1080) {
		t.Errorf("cover wide %v", r)
	}
	if r := Cover(100, 400, 1, 1); r != image.Rect(0, 150, 100, 250) {
		t.Errorf("cover tall %v", r)
	}
}
