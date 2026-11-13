package version

const Current = "v1.1.2"

var Override = ""

func String() string {
	if Override != "" {
		return Override
	}
	return Current
}
