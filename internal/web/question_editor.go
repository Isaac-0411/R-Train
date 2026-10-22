package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"readingtrainer/internal/store"
	"readingtrainer/internal/validate"
)

// defaultOptionCount is the number of answer options a new question starts
// with. The administrator can add and remove options.
const defaultOptionCount = 4

// optionField is one row of the option editor.
type optionField struct {
	Index  int // one based position of the option
	Letter string
	Text   string
}

// questionEditor holds the values of the question editor.
type questionEditor struct {
	Prompt       string
	Options      []string
	CorrectIndex int
}

// newQuestionEditor returns the values of an empty question.
func newQuestionEditor() questionEditor {
	return questionEditor{Options: make([]string, defaultOptionCount)}
}

// questionEditorFor returns the values of an existing question.
func questionEditorFor(q *store.Question) questionEditor {
	return questionEditor{
		Prompt:       q.Prompt,
		Options:      q.Options,
		CorrectIndex: q.CorrectIndex,
	}
}

// optionFields builds the rows of the option editor.
func optionFields(values []string) []optionField {
	fields := make([]optionField, 0, len(values))
	for i, text := range values {
		fields = append(fields, optionField{
			Index:  i + 1,
			Letter: store.Letter(i + 1),
			Text:   text,
		})
	}
	return fields
}

// readQuestionForm reads and validates the question form fields.
//
// The administrator chooses how many options the question has: the form
// sends option_count together with the fields option_1 … option_N. The
// correct answer is the position of the chosen option.
func readQuestionForm(r *http.Request) (questionEditor, validate.Errors) {
	editor := questionEditor{}
	validationErrors := validate.New()

	editor.Prompt = strings.TrimSpace(r.PostFormValue("prompt"))
	if msg := validate.Prompt(editor.Prompt); msg != "" {
		validationErrors.Add("prompt", msg)
	}

	count, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("option_count")))
	if err != nil || count < store.MinOptions {
		validationErrors.Add("option_count",
			fmt.Sprintf("A question needs at least %d answer options.", store.MinOptions))
		return editor, validationErrors
	}
	if count > store.MaxOptions {
		validationErrors.Add("option_count",
			fmt.Sprintf("A question may have at most %d answer options.", store.MaxOptions))
		return editor, validationErrors
	}

	editor.Options = make([]string, 0, count)
	for i := 1; i <= count; i++ {
		text := strings.TrimSpace(r.PostFormValue(fmt.Sprintf("option_%d", i)))
		if msg := validate.Option(text); msg != "" {
			validationErrors.Add(fmt.Sprintf("option_%d", i), msg)
		}
		editor.Options = append(editor.Options, text)
	}

	selected, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("correct_option")))
	if selected < 1 || selected > count {
		validationErrors.Add("correct_option", "Please choose which option is correct.")
	} else {
		editor.CorrectIndex = selected
	}

	return editor, validationErrors
}

// renderLessonForm renders the lesson editor together with the values of the
// question editor.
func (s *Server) renderLessonForm(w http.ResponseWriter, r *http.Request, status int,
	data map[string]any, editor questionEditor) {

	data["PromptValue"] = editor.Prompt
	data["OptionFields"] = optionFields(editor.Options)
	data["CorrectIndex"] = editor.CorrectIndex
	s.render(w, r, status, "admin_lesson_form", data)
}
