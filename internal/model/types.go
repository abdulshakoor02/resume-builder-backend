package model

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type CreateResumeResponse struct {
	ResumeID string `json:"resume_id"`
	PDFURL   string `json:"pdf_url"`
}

type RefineResumeRequest struct {
	Prompt string `json:"prompt"`
}

type RefineResumeResponse struct {
	PDFURL string `json:"pdf_url"`
}

type UploadFileResponse struct {
	FileID        string `json:"file_id"`
	FileName      string `json:"filename"`
	NextcloudPath string `json:"nextcloud_path"`
	ExtractedText string `json:"extracted_text"`
}

type UsageResponse struct {
	ResumesCreated    int  `json:"resumes_created"`
	TotalRevisions    int  `json:"total_revisions"`
	FreeResumeLimit   int  `json:"free_resume_limit"`
	FreeRevisionLimit int  `json:"free_revision_limit"`
	CanCreate         bool `json:"can_create"`
	CanRevise         bool `json:"can_revise"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
