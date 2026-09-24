package models

type Node struct {
	Resource string  `json:"resource"`
	Title    string  `json:"title"`
	Links    []*Node `json:"links"`
}
