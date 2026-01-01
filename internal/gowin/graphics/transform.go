package graphics

// Mat4 is a column-major 4x4 matrix compatible with OpenGL uniforms.
type Mat4 [16]float32

func IdentityMat4() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

func TranslateMat4(x, y float32) Mat4 {
	m := IdentityMat4()
	m[12] = x
	m[13] = y
	return m
}

func ScaleMat4(x, y float32) Mat4 {
	m := IdentityMat4()
	m[0] = x
	m[5] = y
	return m
}

// MulMat4 returns a*b (column-major, vectors on the right).
func MulMat4(a, b Mat4) Mat4 {
	var r Mat4
	for c := 0; c < 4; c++ {
		for row := 0; row < 4; row++ {
			r[c*4+row] =
				a[0*4+row]*b[c*4+0] +
					a[1*4+row]*b[c*4+1] +
					a[2*4+row]*b[c*4+2] +
					a[3*4+row]*b[c*4+3]
		}
	}
	return r
}
