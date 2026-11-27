// Package web serves the three HTML pages with Gin, html/template and one
// CSS file. Everything is plain links and forms, with no JavaScript.
//
// Routes:
//
//	GET  /                      redirect to /expenses
//	GET  /expenses              list for ?month=YYYY-MM (default: current month), optional &category=ID
//	GET  /expenses/new          add form; &mode=pair shows two expenses saved together
//	POST /expenses              create one expense, or two (mode=pair) in one transaction
//	GET  /expenses/{id}/edit    edit form (the same template as add)
//	POST /expenses/{id}         update
//	GET  /summary               category totals for ?month=YYYY-MM
//	GET  /static/style.css      the stylesheet
//
// Form handling: the handlers parse input with expense.Input.Parse and save
// it with expense.Store. Any *expense.ValidationError, found with errors.As,
// is shown next to its input with status 422, and the submitted values are
// kept. A successful save redirects with 303 See Other to the list for the
// saved expense's month. Other errors are logged and give a 500.
//
// Templates (templates/*.html) and the stylesheet (static/style.css) are
// embedded in the binary.
package web

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/NikitaPash/expence-log/internal/expense"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Server holds the parsed templates and the store behind the pages. Create
// one with [NewServer] and serve [Server.Handler].
type Server struct {
	store *expense.Store
	pages map[string]*template.Template
	now   func() time.Time
}

// NewServer parses the embedded templates and returns a Server that reads
// and writes through store. It fails only if a template does not parse.
func NewServer(store *expense.Store) (*Server, error) {
	funcs := template.FuncMap{
		"money":      expense.FormatCents,
		"monthParam": func(t time.Time) string { return t.Format(expense.MonthLayout) },
		"monthTitle": func(t time.Time) string { return t.Format("January 2006") },
		"inc":        func(i int) int { return i + 1 },
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"list", "form", "summary"} {
		t, err := template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return &Server{store: store, pages: pages, now: time.Now}, nil
}

// Handler returns a Gin engine with every route in the package documentation
// registered, plus request logging and panic recovery. Pass it to
// [net/http.ListenAndServe] or call it directly with httptest in tests.
func (s *Server) Handler() http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	static, _ := fs.Sub(staticFS, "static")
	r.StaticFS("/static", http.FS(static))
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, "/expenses") })
	r.GET("/expenses", s.list)
	r.GET("/expenses/new", s.newForm)
	r.POST("/expenses", s.create)
	r.GET("/expenses/:id/edit", s.editForm)
	r.POST("/expenses/:id", s.update)
	r.GET("/summary", s.summary)
	return r
}

func (s *Server) render(c *gin.Context, status int, page string, data gin.H) {
	var buf bytes.Buffer
	if err := s.pages[page].ExecuteTemplate(&buf, "layout", data); err != nil {
		s.fail(c, err)
		return
	}
	c.Data(status, "text/html; charset=utf-8", buf.Bytes())
}

func (s *Server) fail(c *gin.Context, err error) {
	log.Printf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	c.String(http.StatusInternalServerError, "Something went wrong. Check the server log.")
}

// month reads the month parameter (query or form), defaulting to the current month.
func (s *Server) month(c *gin.Context) (time.Time, bool) {
	raw := c.Query("month")
	if c.Request.Method == http.MethodPost {
		raw = c.PostForm("month")
	}
	if raw == "" {
		return expense.MonthOf(s.now()), true
	}
	m, err := expense.ParseMonth(raw)
	if err != nil {
		c.String(http.StatusBadRequest, "month must be in YYYY-MM format")
		return time.Time{}, false
	}
	return m, true
}

func monthURL(path string, m time.Time, extra url.Values) string {
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("month", m.Format(expense.MonthLayout))
	return path + "?" + q.Encode()
}

// withNav adds the month and previous/next month links to data.
func withNav(data gin.H, path string, m time.Time, extra url.Values) gin.H {
	prev, next := m.AddDate(0, -1, 0), m.AddDate(0, 1, 0)
	data["Month"], data["Prev"], data["Next"] = m, prev, next
	data["PrevURL"], data["NextURL"] = monthURL(path, prev, extra), monthURL(path, next, extra)
	return data
}

func (s *Server) list(c *gin.Context) {
	m, ok := s.month(c)
	if !ok {
		return
	}
	var catID int64
	extra := url.Values{}
	if raw := c.Query("category"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			c.String(http.StatusBadRequest, "category must be a category ID")
			return
		}
		catID = id
		extra.Set("category", raw)
	}
	cats, err := s.store.Categories(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	rows, err := s.store.List(c, m, catID)
	if err != nil {
		s.fail(c, err)
		return
	}
	var total int64
	for _, e := range rows {
		total += e.AmountCents
	}
	s.render(c, http.StatusOK, "list", withNav(gin.H{
		"Title": "Expenses", "Categories": cats, "CategoryID": catID,
		"Expenses": rows, "TotalCents": total,
	}, "/expenses", m, extra))
}

// formRow is one expense's submitted values and per-field error messages.
type formRow struct {
	Values expense.Input
	Errors map[string]string
}

func (s *Server) renderForm(c *gin.Context, status int, title, action, mode string, m time.Time, rows []formRow, formError string) {
	cats, err := s.store.Categories(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, status, "form", gin.H{
		"Title": title, "Action": action, "Mode": mode, "Month": m,
		"Rows": rows, "Categories": cats, "FormError": formError,
	})
}

func formTitle(mode string) string {
	if mode == "pair" {
		return "Add two expenses"
	}
	return "Add expense"
}

func (s *Server) newForm(c *gin.Context) {
	m, ok := s.month(c)
	if !ok {
		return
	}
	// Default the date to today when viewing the current month, otherwise the 1st.
	date := m
	if today := s.now(); expense.MonthOf(today).Equal(m) {
		date = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	}
	blank := formRow{Values: expense.Input{Date: date.Format(expense.DateLayout)}}
	mode, rows := "single", []formRow{blank}
	if c.Query("mode") == "pair" {
		mode, rows = "pair", []formRow{blank, blank}
	}
	s.renderForm(c, http.StatusOK, formTitle(mode), "/expenses", mode, m, rows, "")
}

// readRows returns the submitted rows. The form repeats each field name once per row.
func readRows(c *gin.Context, want int) ([]formRow, bool) {
	dates, amounts := c.PostFormArray("date"), c.PostFormArray("amount")
	cats, descs := c.PostFormArray("category_id"), c.PostFormArray("description")
	if len(dates) != want || len(amounts) != want || len(cats) != want || len(descs) != want {
		return nil, false
	}
	rows := make([]formRow, want)
	for i := range rows {
		rows[i].Values = expense.Input{Date: dates[i], Amount: amounts[i], CategoryID: cats[i], Description: descs[i]}
	}
	return rows, true
}

// fieldErrors maps each *expense.ValidationError inside err to field -> reason.
// It returns nil when err holds no validation errors.
func fieldErrors(err error) map[string]string {
	list := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		list = joined.Unwrap()
	}
	var out map[string]string
	for _, e := range list {
		var ve *expense.ValidationError
		if errors.As(e, &ve) {
			if out == nil {
				out = map[string]string{}
			}
			out[ve.Field] = ve.Reason
		}
	}
	return out
}

// parseRows validates every row, recording field errors on each. It reports
// whether all rows are valid.
func parseRows(rows []formRow) ([]expense.Expense, bool) {
	out := make([]expense.Expense, len(rows))
	valid := true
	for i := range rows {
		e, err := rows[i].Values.Parse()
		if err != nil {
			rows[i].Errors = fieldErrors(err)
			valid = false
		}
		out[i] = e
	}
	return out, valid
}

func (s *Server) create(c *gin.Context) {
	m, ok := s.month(c)
	if !ok {
		return
	}
	mode, want := "single", 1
	if c.PostForm("mode") == "pair" {
		mode, want = "pair", 2
	}
	rows, ok := readRows(c, want)
	if !ok {
		c.String(http.StatusBadRequest, "form must contain %d complete expense row(s)", want)
		return
	}
	expenses, valid := parseRows(rows)
	if !valid {
		s.renderForm(c, http.StatusUnprocessableEntity, formTitle(mode), "/expenses", mode, m, rows, "Please fix the highlighted fields.")
		return
	}

	var err error
	if want == 1 {
		_, err = s.store.Create(c, expenses[0])
	} else {
		_, err = s.store.CreateBatch(c, expenses)
	}
	if err != nil {
		// A batch error says which row failed; a single create is row 0.
		row := 0
		var be *expense.BatchError
		if errors.As(err, &be) {
			row = be.Row
		}
		if fe := fieldErrors(err); fe != nil {
			rows[row].Errors = fe
			msg := "Please fix the highlighted fields."
			if want > 1 {
				msg = "Nothing was saved. Please fix the highlighted fields."
			}
			s.renderForm(c, http.StatusUnprocessableEntity, formTitle(mode), "/expenses", mode, m, rows, msg)
			return
		}
		s.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, monthURL("/expenses", expense.MonthOf(expenses[0].Date), nil))
}

func (s *Server) loadExpense(c *gin.Context) (expense.Expense, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.String(http.StatusNotFound, "expense not found")
		return expense.Expense{}, false
	}
	e, err := s.store.Get(c, id)
	if errors.Is(err, expense.ErrNotFound) {
		c.String(http.StatusNotFound, "expense not found")
		return expense.Expense{}, false
	}
	if err != nil {
		s.fail(c, err)
		return expense.Expense{}, false
	}
	return e, true
}

func (s *Server) editForm(c *gin.Context) {
	e, ok := s.loadExpense(c)
	if !ok {
		return
	}
	s.renderForm(c, http.StatusOK, "Edit expense", "/expenses/"+strconv.FormatInt(e.ID, 10), "edit",
		expense.MonthOf(e.Date), []formRow{{Values: expense.InputFrom(e)}}, "")
}

func (s *Server) update(c *gin.Context) {
	existing, ok := s.loadExpense(c)
	if !ok {
		return
	}
	action := "/expenses/" + strconv.FormatInt(existing.ID, 10)
	m := expense.MonthOf(existing.Date)
	rows, ok := readRows(c, 1)
	if !ok {
		c.String(http.StatusBadRequest, "form must contain one complete expense row")
		return
	}
	parsed, valid := parseRows(rows)
	if !valid {
		s.renderForm(c, http.StatusUnprocessableEntity, "Edit expense", action, "edit", m, rows, "Please fix the highlighted fields.")
		return
	}
	e := parsed[0]
	e.ID = existing.ID
	if err := s.store.Update(c, e); err != nil {
		if fe := fieldErrors(err); fe != nil {
			rows[0].Errors = fe
			s.renderForm(c, http.StatusUnprocessableEntity, "Edit expense", action, "edit", m, rows, "Please fix the highlighted fields.")
			return
		}
		s.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, monthURL("/expenses", expense.MonthOf(e.Date), nil))
}

func (s *Server) summary(c *gin.Context) {
	m, ok := s.month(c)
	if !ok {
		return
	}
	sum, err := s.store.MonthlySummary(c, m)
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, http.StatusOK, "summary", withNav(gin.H{
		"Title": "Monthly summary", "Summary": sum,
	}, "/summary", m, nil))
}
