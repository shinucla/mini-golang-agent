package version

const Current = "v1.0.12"

var Override = ""

func String() string {
	if Override != "" {
		return Override
	}
	return Current
}
