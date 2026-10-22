package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"readingtrainer/internal/store"
)

// questionsPerBox is the number of questions shown in one box of the quiz.
const questionsPerBox = 5

// QuestionBox is one page of the quiz. The lesson text stays on screen while
// the user moves between the boxes with the arrow buttons.
type QuestionBox struct {
	Index     int
	Number    int
	From      int
	To        int
	Questions []store.Question
}

// buildBoxes splits the questions of a lesson into boxes of five.
func buildBoxes(questions []store.Question) []QuestionBox {
	boxes := []QuestionBox{}
	for start := 0; start < len(questions); start += questionsPerBox {
		end := start + questionsPerBox
		if end > len(questions) {
			end = len(questions)
		}
		boxes = append(boxes, QuestionBox{
			Index:     len(boxes),
			Number:    len(boxes) + 1,
			From:      start + 1,
			To:        end,
			Questions: questions[start:end],
		})
	}
	return boxes
}

// handleLanding renders the landing page with the lesson buttons.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.notFound(w, r)
		return
	}

	lessons, err := s.store.ListLessons(r.Context())
	if err != nil {
		s.logger.Error("list lessons", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lessons could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "landing", map[string]any{
		"Title":   "Lessons",
		"Lessons": lessons,
	})
}

// handleLessonText renders the Myanmar language page: the text of the lesson
// with a button that leads to the questions.
func (s *Server) handleLessonText(w http.ResponseWriter, r *http.Request) {
	lesson, ok := s.loadLesson(w, r)
	if !ok {
		return
	}

	count, err := s.store.CountQuestions(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("count questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The lesson could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "lesson_text", map[string]any{
		"Title":         lesson.Title,
		"Lesson":        lesson,
		"QuestionCount": count,
	})
}

// handleQuiz renders the questions of a lesson.
func (s *Server) handleQuiz(w http.ResponseWriter, r *http.Request) {
	lesson, ok := s.loadLesson(w, r)
	if !ok {
		return
	}

	questions, err := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("list questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The questions could not be loaded.")
		return
	}

	if len(questions) == 0 {
		s.setFlash(r, "error", "This lesson does not have any questions yet.")
		http.Redirect(w, r, fmt.Sprintf("/lessons/%d", lesson.ID), http.StatusSeeOther)
		return
	}

	s.render(w, r, http.StatusOK, "quiz", map[string]any{
		"Title":   lesson.Title,
		"Lesson":  lesson,
		"Boxes":   buildBoxes(questions),
		"Total":   len(questions),
		"BoxSize": questionsPerBox,
	})
}

// handleSubmit grades the answers of a lesson and stores the result.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	lesson, ok := s.loadLesson(w, r)
	if !ok {
		return
	}

	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	questions, err := s.store.ListQuestionsByLesson(r.Context(), lesson.ID)
	if err != nil {
		s.logger.Error("list questions", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The questions could not be loaded.")
		return
	}

	if len(questions) == 0 {
		s.setFlash(r, "error", "This lesson does not have any questions yet.")
		http.Redirect(w, r, fmt.Sprintf("/lessons/%d", lesson.ID), http.StatusSeeOther)
		return
	}

	// Collect the answers. The score is always calculated on the server, the
	// browser never decides what is correct.
	answers := make([]store.AttemptAnswer, 0, len(questions))
	missing := []string{}
	score := 0

	for _, question := range questions {
		field := fmt.Sprintf("q_%d", question.ID)
		selected := strings.ToLower(strings.TrimSpace(r.PostFormValue(field)))
		index := store.IndexOfLetter(selected)
		if index < 1 || index > len(question.Options) {
			missing = append(missing, strconv.Itoa(len(answers)+1))
			answers = append(answers, store.AttemptAnswer{
				QuestionID: question.ID,
			})
			continue
		}

		correct := index == question.CorrectIndex
		if correct {
			score++
		}
		answers = append(answers, store.AttemptAnswer{
			QuestionID:     question.ID,
			SelectedOption: selected,
			IsCorrect:      correct,
		})
	}

	if len(missing) > 0 {
		s.render(w, r, http.StatusBadRequest, "quiz", map[string]any{
			"Title":     lesson.Title,
			"Lesson":    lesson,
			"Boxes":     buildBoxes(questions),
			"Total":     len(questions),
			"BoxSize":   questionsPerBox,
			"Message":   "Please answer every question before you submit.",
			"Missing":   missing,
			"ShowAlert": true,
		})
		return
	}

	attempt, err := s.store.CreateAttempt(r.Context(), user.ID, lesson.ID, score, len(questions), answers)
	if err != nil {
		s.logger.Error("create attempt", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The result could not be saved.")
		return
	}

	// POST/redirect/GET keeps a refresh of the result page safe.
	http.Redirect(w, r, fmt.Sprintf("/results/%d", attempt.ID), http.StatusSeeOther)
}

// handleAttempts renders the result history of the signed in user.
func (s *Server) handleAttempts(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	attempts, err := s.store.ListAttemptsByUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list attempts", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The results could not be loaded.")
		return
	}

	stats, err := s.store.StatsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("user stats", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The results could not be loaded.")
		return
	}

	s.render(w, r, http.StatusOK, "attempts", map[string]any{
		"Title":    "My results",
		"Attempts": attempts,
		"Stats":    stats,
	})
}

// handleAttemptDetail renders a single result. Users see their own results,
// administrators may look at anybody's result.
func (s *Server) handleAttemptDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}

	attempt, err := s.store.GetAttempt(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.logger.Error("get attempt", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "The result could not be loaded.")
		return
	}

	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if attempt.UserID != user.ID && !user.IsAdmin() {
		s.renderError(w, r, http.StatusForbidden, "You may only look at your own results.")
		return
	}

	s.render(w, r, http.StatusOK, "result", map[string]any{
		"Title":   "Result",
		"Attempt": attempt,
	})
}
