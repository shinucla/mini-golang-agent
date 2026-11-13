package version

const Current = "v1.0.13"

var Override = ""

func String() string {
	if Override != "" {
		return Override
	}
	return Current
}
