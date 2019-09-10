package common

// Structure used to generate the json payload for the CircleCI api call
type CircleApiPayload struct {
	Branch          string          `json:"branch"`
	BuildParameters BuildParameters `json:"parameters"`
}

// Contains the parameters for the CircleCI build json
type BuildParameters struct {
	CommitHash  string `json:"commit-hash"`
	Blocks      string `json:"blocks"`
	Genesis     string `json:"genesis"`
	Integration string `json:"integration"`
}

// Structure used to unmarshal the event payload received from GitHub
type GithubEventPayload struct {
	Issue   Issue   `json:"issue"`
	Comment Comment `json:"comment"`
	Repo    Repo    `json:"repository"`
}

// The fields corresponding to the GitHub Issue object
type Issue struct {
	Number int `json:"number"`
	Pr     Pr  `json:"pull_request,omitempty"`
}

// The fields corresponding to the GitHub Pr object
type Pr struct {
	Url string `json:"url,omitempty"`
}

// The fields corresponding to the GitHub comment object
type Comment struct {
	Body string `json:"body"`
}

// The fields corresponding to the GitHub Repo object
type Repo struct {
	Name  string `json:"name"`
	Owner Owner  `json:"owner"`
}

// The fields corresponding to the GitHub Owner object
type Owner struct {
	Login string `json:"login"`
}
