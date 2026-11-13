package version

const Current = "v1.1.1"

var Override = ""

func String() string {
	if Override != "" {
		return Override
	}
	return Current
}
