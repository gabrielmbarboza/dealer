package dealer

type Info struct {
	ProjectName string `json:"project_name"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Version     string `json:"version"`
}

func ProjectInfo() Info {
	return Info{
		ProjectName: "Dealer API Gateway",
		Description: "A basic API Gateway developed with Go",
		Author:      "Gabriel Barboza",
		Version:     Version,
	}
}
