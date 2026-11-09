/* ==========================================================================
   Reading Trainer — client side behaviour
   The server always validates again; this only improves the experience.
   ========================================================================== */

(function () {
  "use strict";

  /* ---------------------------------------------------------------- quiz */

  function initQuiz() {
    var form = document.getElementById("quizForm");
    if (!form) return;

    var boxes = Array.prototype.slice.call(form.querySelectorAll(".qbox"));
    if (boxes.length === 0) return;

    var label = document.getElementById("boxLabel");
    var prevBtn = document.getElementById("prevBox");
    var nextBtn = document.getElementById("nextBox");
    var hint = document.getElementById("quizHint");
    var progress = document.getElementById("boxProgress");
    var progressBar = document.getElementById("boxProgressBar");
    var answerCount = document.getElementById("answerCount");
    var questionMap = form.querySelector(".question-map");
    var jumps = Array.prototype.slice.call(form.querySelectorAll("[data-question-index]"));
    var nextQuestion = document.getElementById("nextQuestion");
    var submitBtn = document.getElementById("submitBtn");
    var current = 0;

    if (questionMap) questionMap.hidden = false;

    function updateAnswers() {
      var answered = 0;
      boxes.forEach(function (box, i) {
        var complete = !!box.querySelector("input[type=radio]:checked");
        if (complete) answered++;
        if (jumps[i]) {
          jumps[i].classList.toggle("is-answered", complete);
          jumps[i].setAttribute("aria-label", "Question " + (i + 1) + (complete ? ", answered" : ", unanswered"));
        }
      });
      if (answerCount) answerCount.textContent = answered + " of " + boxes.length + " answered";
      if (progress) {
        progress.setAttribute("aria-valuenow", String(answered));
        progress.setAttribute("aria-valuetext", answered + " of " + boxes.length + " answered");
      }
      if (progressBar) progressBar.style.width = (answered / boxes.length * 100) + "%";
      return answered;
    }

    // The arrow buttons move through the questions. The page only scrolls
    // when the question card is not on the screen, so the text can be read
    // in peace: nothing jumps around while the card is already visible, and
    // the step, the arrows and the progress stay in reach.
    function ensureQuestionVisible(box) {
      if (!box) return;

      var target = box.closest ? (box.closest(".questions-panel") || box) : box;
      var topbar = document.querySelector(".topbar");
      var margin = (topbar ? topbar.getBoundingClientRect().bottom : 0) + 16;
      var rect = target.getBoundingClientRect();

      if (rect.top >= margin && rect.top <= window.innerHeight) return;

      var top = rect.top + window.pageYOffset - margin;
      if (top < 0) top = 0;
      var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      window.scrollTo({ top: top, behavior: reduceMotion ? "instant" : "smooth" });
    }

    function showBox(index, scroll) {
      if (index < 0) index = 0;
      if (index > boxes.length - 1) index = boxes.length - 1;
      current = index;

      boxes.forEach(function (box, i) {
        var active = i === index;
        box.hidden = !active;
        box.classList.toggle("is-active", active);
      });

      if (label) label.textContent = "Question " + (index + 1) + " of " + boxes.length;
      if (prevBtn) prevBtn.disabled = index === 0;
      if (nextBtn) nextBtn.disabled = index === boxes.length - 1;
      if (nextQuestion) nextQuestion.hidden = index === boxes.length - 1;
      if (submitBtn) {
        submitBtn.classList.toggle("btn-ghost", index !== boxes.length - 1);
        submitBtn.classList.toggle("btn-primary", index === boxes.length - 1);
      }
      jumps.forEach(function (jump, i) {
        if (i === index) jump.setAttribute("aria-current", "step");
        else jump.removeAttribute("aria-current");
      });

      if (scroll) {
        ensureQuestionVisible(boxes[index]);
        var prompt = boxes[index].querySelector(".question-text");
        if (prompt) prompt.focus({ preventScroll: true });
      }
    }

    if (prevBtn) prevBtn.addEventListener("click", function () { showBox(current - 1, true); });
    if (nextBtn) nextBtn.addEventListener("click", function () { showBox(current + 1, true); });
    if (nextQuestion) nextQuestion.addEventListener("click", function () { showBox(current + 1, true); });
    jumps.forEach(function (jump) {
      jump.addEventListener("click", function () {
        showBox(Number(jump.getAttribute("data-question-index")), true);
      });
    });

    document.addEventListener("keydown", function (event) {
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      if (event.target && (/^(INPUT|TEXTAREA|SELECT|BUTTON|A)$/.test(event.target.tagName) || event.target.isContentEditable)) return;
      if (event.key === "ArrowLeft") { event.preventDefault(); showBox(current - 1, true); }
      if (event.key === "ArrowRight") { event.preventDefault(); showBox(current + 1, true); }
    });

    // Answer every question before the form may be submitted.
    form.addEventListener("submit", function (event) {
      var missing = [];
      var questions = form.querySelectorAll(".question");
      questions.forEach(function (question, i) {
        var radios = question.querySelectorAll("input[type=radio]");
        var answered = false;
        for (var r = 0; r < radios.length; r++) {
          if (radios[r].checked) { answered = true; break; }
        }
        if (!answered) {
          missing.push(i + 1);
          question.classList.add("is-missing");
        }
      });

      if (missing.length > 0) {
        event.preventDefault();
        if (hint) {
          hint.textContent = missing.length === 1
            ? "Question " + missing[0] + " still needs an answer."
            : missing.length + " questions still need an answer: " + missing.join(", ");
          hint.classList.add("is-error");
        }
        // Jump to the first box that is not finished yet.
        for (var i = 0; i < boxes.length; i++) {
          var unanswered = boxes[i].querySelector(".question.is-missing");
          if (unanswered) {
            showBox(i, true);
            break;
          }
        }
        return;
      }

      if (hint) { hint.textContent = ""; hint.classList.remove("is-error"); }
    });

    // Clear the "missing" marker as soon as the learner answers.
    form.addEventListener("change", function (event) {
      var question = event.target.closest ? event.target.closest(".question") : null;
      if (question) question.classList.remove("is-missing");
      var answered = updateAnswers();
      if (hint && hint.classList.contains("is-error")) {
        hint.textContent = answered === boxes.length ? "All answered. Ready to check your answers." : (boxes.length - answered) + " questions still need an answer.";
        hint.classList.toggle("is-error", answered !== boxes.length);
      }
    });

    showBox(0, false);
    // Browsers may restore radio selections when returning to a quiz.
    updateAnswers();
    window.addEventListener("pageshow", updateAnswers);
  }

  /* --------------------------------------------------------- reading tools */

  function initStudyTools() {
    var toolbar = document.querySelector("[data-study-tools]");
    if (!toolbar) return;
    toolbar.hidden = false;

    var sizes = toolbar.querySelectorAll("[data-reading-size]");
    function paintSize(size) {
      document.documentElement.setAttribute("data-reading-size", size);
      sizes.forEach(function (button) {
        button.setAttribute("aria-pressed", String(button.getAttribute("data-reading-size") === size));
      });
    }
    paintSize(document.documentElement.getAttribute("data-reading-size") || "normal");
    sizes.forEach(function (button) {
      button.addEventListener("click", function () {
        var size = button.getAttribute("data-reading-size");
        paintSize(size);
        try { window.localStorage.setItem("reading-size", size); } catch (error) { /* Use this size for the current page. */ }
      });
    });

    var focus = document.getElementById("focusToggle");
    if (focus) {
      function paintFocus(active) {
        document.body.classList.toggle("study-focus", active);
        focus.setAttribute("aria-pressed", String(active));
        focus.textContent = active ? "Exit focus view" : "Focus view";
      }
      try { paintFocus(window.sessionStorage.getItem("study-focus") === "true"); } catch (error) { /* Start with the regular view. */ }
      focus.addEventListener("click", function () {
        var active = !document.body.classList.contains("study-focus");
        paintFocus(active);
        try { window.sessionStorage.setItem("study-focus", String(active)); } catch (error) { /* Keep the current view. */ }
      });
    }
  }

  /* ---------------------------------------------------------- dark mode */

  // The button in the top bar switches between the light and the dark
  // theme. The choice is kept in the browser, without a choice the system
  // setting of the device is used.
  function initThemeToggle() {
    var button = document.getElementById("themeToggle");
    if (!button) return;

    function storedTheme() {
      try {
        return window.localStorage.getItem("theme");
      } catch (error) {
        return null;
      }
    }

    function systemTheme() {
      if (!window.matchMedia) return "light";
      return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }

    function currentTheme() {
      var applied = document.documentElement.getAttribute("data-theme");
      if (applied === "light" || applied === "dark") return applied;
      var theme = storedTheme();
      if (theme === "light" || theme === "dark") return theme;
      return systemTheme();
    }

    function paint(theme) {
      var next = theme === "dark" ? "light" : "dark";
      button.textContent = theme === "dark" ? "☀" : "☾";
      button.setAttribute("title", "Switch to " + next + " mode");
      button.setAttribute("aria-label", "Switch to " + next + " mode");
    }

    paint(currentTheme());

    button.addEventListener("click", function () {
      var theme = currentTheme() === "dark" ? "light" : "dark";
      document.documentElement.setAttribute("data-theme", theme);
      try {
        window.localStorage.setItem("theme", theme);
      } catch (error) {
        /* Without storage the theme is only valid for this page. */
      }
      paint(theme);
    });

    // Follow the system setting as long as nothing was chosen by hand.
    if (window.matchMedia) {
      var query = window.matchMedia("(prefers-color-scheme: dark)");
      var listener = function () {
        if (!storedTheme()) paint(currentTheme());
      };
      if (query.addEventListener) {
        query.addEventListener("change", listener);
      } else if (query.addListener) {
        query.addListener(listener);
      }
    }
  }

  /* ------------------------------------------------------- password toggle */

  function initPasswordToggles() {
    var buttons = document.querySelectorAll("[data-toggle-password]");
    Array.prototype.forEach.call(buttons, function (button) {
      button.addEventListener("click", function () {
        var field = document.getElementById(button.getAttribute("data-toggle-password"));
        if (!field) {
          // The field has no id, so it is looked up next to the button.
          var wrapper = button.closest ? button.closest(".input-with-button") : null;
          field = wrapper ? wrapper.querySelector("input") : null;
        }
        if (!field || (field.type !== "password" && field.type !== "text")) return;

        var hidden = field.type === "password";
        field.type = hidden ? "text" : "password";
        button.textContent = hidden ? "Hide" : "Show";
        button.setAttribute("aria-pressed", hidden ? "true" : "false");
      });
    });
  }

  /* --------------------------------------------------------- confirm boxes */

  function initConfirmations() {
    document.addEventListener("click", function (event) {
      var button = event.target.closest ? event.target.closest("[data-confirm]") : null;
      if (!button) return;
      if (!window.confirm(button.getAttribute("data-confirm"))) {
        event.preventDefault();
        event.stopPropagation();
      }
    });
  }

  /* ------------------------------------------------------ client validation */

  function initValidation() {
    var forms = document.querySelectorAll("[data-validate]");
    Array.prototype.forEach.call(forms, function (form) {
      form.addEventListener("submit", function (event) {
        var username = form.querySelector("[name=username]");
        var password = form.querySelector("[name=password]");
        var confirmation = form.querySelector("[name=password_confirmation]");
        var message = "";

        if (username && username.value.trim().length < 3) {
          message = "The username must be at least 3 characters long.";
        } else if (username && !/^[A-Za-z0-9._-]{3,32}$/.test(username.value.trim())) {
          message = "The username may only contain letters, numbers, dot, dash or underscore.";
        } else if (password && password.value.length < 8) {
          message = "The password must be at least 8 characters long.";
        } else if (confirmation && confirmation.value !== password.value) {
          message = "The passwords do not match.";
        }

        if (message) {
          event.preventDefault();
          window.alert(message);
        }
      });
    });
  }

  /* ------------------------------------------------------- option editor */

  // The question editor lets the administrator choose how many answer options
  // a question has. The rows are renumbered after every change, so the form
  // always sends option_1 … option_N together with option_count.
  function initOptionEditor() {
    var editor = document.getElementById("optionEditor");
    if (!editor) return;

    var min = parseInt(editor.getAttribute("data-min") || "2", 10);
    var max = parseInt(editor.getAttribute("data-max") || "8", 10);
    var countInput = document.getElementById("optionCount");
    var label = document.getElementById("optionCountLabel");
    var addButton = document.getElementById("addOption");

    function rows() {
      return Array.prototype.slice.call(editor.querySelectorAll(".option-row"));
    }

    function letter(index) {
      return String.fromCharCode(65 + index);
    }

    function renumber() {
      var list = rows();

      list.forEach(function (row, i) {
        var input = row.querySelector(".option-row-text");
        var radio = row.querySelector(".option-row-correct input");
        var tag = row.querySelector(".option-letter");
        var remove = row.querySelector("[data-remove-option]");

        if (input) {
          input.name = "option_" + (i + 1);
          input.setAttribute("aria-label", "Option " + letter(i));
        }
        if (radio) radio.value = String(i + 1);
        if (tag) tag.textContent = letter(i);
        if (remove) remove.disabled = list.length <= min;
      });

      if (countInput) countInput.value = list.length;
      if (label) {
        label.textContent = list.length + (list.length === 1 ? " option" : " options");
      }
      if (addButton) addButton.disabled = list.length >= max;
    }

    if (addButton) {
      addButton.addEventListener("click", function () {
        var list = rows();
        if (list.length >= max) return;

        var template = list[list.length - 1];
        if (!template) return;

        var row = template.cloneNode(true);
        var input = row.querySelector(".option-row-text");
        var radio = row.querySelector(".option-row-correct input");
        if (input) input.value = "";
        if (radio) radio.checked = false;

        editor.insertBefore(row, addButton);
        renumber();
        if (input) input.focus();
      });
    }

    editor.addEventListener("click", function (event) {
      var button = event.target.closest ? event.target.closest("[data-remove-option]") : null;
      if (!button) return;

      var list = rows();
      if (list.length <= min) return;

      var row = button.closest(".option-row");
      if (row && row.parentNode) row.parentNode.removeChild(row);
      renumber();
    });

    renumber();
  }

  /* ------------------------------------------------------- session timeout */

  // A quiet keep-alive: while a signed in page is open, real interaction
  // (a click, a key press, a changed field) asks the server to restart the
  // idle clock. A learner who is working through a quiz is therefore never
  // signed out in the middle of it, while an abandoned tab still times out.
  function initSessionKeepAlive() {
    var meta = document.querySelector('meta[name="session-timeout"]');
    if (!meta) return;

    var ttl = parseInt(meta.getAttribute("content"), 10);
    if (!ttl || ttl <= 0) return;

    var tokenInput = document.querySelector('input[name="csrf_token"]');
    if (!tokenInput) return;

    // At most every five minutes, but a third of the timeout when that is
    // shorter (a test or a deliberately strict installation).
    var interval = Math.min(5 * 60, Math.max(30, ttl / 3)) * 1000;
    var last = 0;
    var busy = false;

    function touch() {
      var now = Date.now();
      if (busy || now - last < interval) return;
      last = now;
      busy = true;

      var body = new URLSearchParams();
      body.set("csrf_token", tokenInput.value);

      fetch("/session/touch", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: body.toString(),
        credentials: "same-origin",
        keepalive: true,
      })
        .catch(function () {
          // The next real request reports a timeout; nothing to do here.
        })
        .then(function () {
          busy = false;
        });
    }

    ["pointerdown", "keydown", "change"].forEach(function (name) {
      document.addEventListener(name, touch, { passive: true, capture: true });
    });
  }

  /* ----------------------------------------------------------------- boot */

  function boot() {
    initStudyTools();
    initQuiz();
    initOptionEditor();
    initPasswordToggles();
    initConfirmations();
    initValidation();
    initThemeToggle();
    initSessionKeepAlive();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
