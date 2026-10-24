package main

import "image"

// crop returns the part r of img, in img's coordinates.
func crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	return img.SubImage(r.Intersect(img.Bounds())).(*image.RGBA)
}

// diff counts the pixels of a and b, the same size, whose channels differ by more than tol.
func diff(a, b *image.RGBA, tol int) (differ int, first image.Point) {
	first = image.Pt(-1, -1)
	ab, bb := a.Bounds(), b.Bounds()
	for y := 0; y < min(ab.Dy(), bb.Dy()); y++ {
		for x := 0; x < min(ab.Dx(), bb.Dx()); x++ {
			pa := a.PixOffset(ab.Min.X+x, ab.Min.Y+y)
			pb := b.PixOffset(bb.Min.X+x, bb.Min.Y+y)
			for c := 0; c < 3; c++ {
				d := int(a.Pix[pa+c]) - int(b.Pix[pb+c])
				if d > tol || -d > tol {
					if differ == 0 {
						first = image.Pt(x, y)
					}
					differ++
					break
				}
			}
		}
	}
	return differ, first
}
