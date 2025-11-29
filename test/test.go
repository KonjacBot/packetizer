package test

//type VarIntArray []int32
//
//func (a VarIntArray) WriteTo(w io.Writer) (n int64, err error) {
//	size := len(a)
//	if nn, err := pk.VarInt(size).WriteTo(w); err != nil {
//		return n, err
//	} else {
//		n += nn
//	}
//	for i := 0; i < size; i++ {
//		nn, err := pk.VarInt(a[i]).WriteTo(w)
//		n += nn
//		if err != nil {
//			return n, err
//		}
//	}
//	return n, nil
//}
//
//func (a *VarIntArray) ReadFrom(r io.Reader) (n int64, err error) {
//	var size pk.VarInt
//	nn, err := size.ReadFrom(r)
//	n += nn
//	if err != nil {
//		return n, err
//	}
//	if size < 0 {
//		return n, errors.New("array length less than zero")
//	}
//
//	if cap(*a) >= int(size) {
//		*a = (*a)[:int(size)]
//	} else {
//		*a = make(VarIntArray, int(size))
//	}
//
//	for i := 0; i < int(size); i++ {
//		nn, err = (*pk.VarInt)(&(*a)[i]).ReadFrom(r)
//		n += nn
//		if err != nil {
//			return n, err
//		}
//	}
//
//	return n, err
//}

//codec:gen
type WaypointColor struct {
	R, G, B uint8
}

//codec:gen
type WaypointVec3i struct {
	X, Y, Z int32 `mc:"VarInt"`
}

//codec:gen
type WaypointChunkPos struct {
	X, Z int32 `mc:"VarInt"`
}

//codec:gen
type WaypointAzimuth struct {
	Angle float32
}

//
//codec:gen
type ExampleEnum struct {
	WaypointType int32 `mc:"VarInt"`
	//ExampleWaypoint
	Waypoint any
}

//
////codec:gen
//type ExampleStruct struct {
//}
