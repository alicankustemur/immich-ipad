package main

type searchAsset struct {
	ID               string `json:"id"`
	FileCreatedAt    string `json:"fileCreatedAt"`
	OriginalFileName string `json:"originalFileName"`
}

type searchResponse struct {
	Assets struct {
		Items    []searchAsset `json:"items"`
		NextPage string       `json:"nextPage"`
	} `json:"assets"`
}
