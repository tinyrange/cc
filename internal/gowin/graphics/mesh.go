package graphics

// Vertex matches the graphics shader input layout:
//
//	a_position: vec2
//	a_texCoord: vec2
//	a_color:    vec4
//
// All values are in float32 and packed tightly in this order.
type Vertex struct {
	X float32
	Y float32
	U float32
	V float32
	R float32
	G float32
	B float32
	A float32
}

// Mesh is an opaque GPU resource created by Window.NewMesh and drawn by Frame.RenderMesh.
type Mesh interface {
	isMesh()
}

type DrawOptions struct {
	// Model is a column-major 4x4 model transform applied to vertex positions
	// before projection. If left as the zero value, Identity is assumed.
	Model Mat4
}
