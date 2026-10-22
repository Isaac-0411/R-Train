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
    var current = 0;

    // The arrow buttons move through the questions. The page only scrolls
    // when the question is not on the screen, so the text can be read in
    // peace: nothing jumps around while the question is already visible.
    function ensureQuestionVisible(box) {
      if (!box) return;

      var topbar = document.querySelector(".topbar");
      var margin = (topbar ? topbar.getBoundingClientRect().bottom : 0) + 16;
      var rect = box.getBoundingClientRect();

      if (rect.top >= margin && rect.bottom <= window.innerHeight) return;

      var top = rect.top + window.pageYOffset - margin;
      if (top < 0) top = 0;
      window.scrollTo({ top: top, behavior: "smooth" });
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

      if (scroll) ensureQuestionVisible(boxes[index]);
    }

    if (prevBtn) prevBtn.addEventListener("click", function () { showBox(current - 1, true); });
    if (nextBtn) nextBtn.addEventListener("click", function () { showBox(current + 1, true); });

    document.addEventListener("keydown", function (event) {
      if (event.target && /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName)) return;
      if (event.key === "ArrowLeft") { showBox(current - 1, true); }
      if (event.key === "ArrowRight") { showBox(current + 1, true); }
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
    });

    showBox(0, false);
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
