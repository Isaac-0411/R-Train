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
    var range = document.getElementById("boxRange");
    var prevBtn = document.getElementById("prevBox");
    var nextBtn = document.getElementById("nextBox");
    var hint = document.getElementById("quizHint");
    var current = 0;

    function showBox(index) {
      if (index < 0) index = 0;
      if (index > boxes.length - 1) index = boxes.length - 1;
      current = index;

      boxes.forEach(function (box, i) {
        var active = i === index;
        box.hidden = !active;
        box.classList.toggle("is-active", active);
      });

      if (label) label.textContent = "Box " + (index + 1) + " of " + boxes.length;
      if (range) {
        var box = boxes[index];
        var from = box.getAttribute("data-from");
        var to = box.getAttribute("data-to");
        var total = box.getAttribute("data-total");
        range.textContent = "Questions " + from + "–" + to + " of " + total;
      }
      if (prevBtn) prevBtn.disabled = index === 0;
      if (nextBtn) nextBtn.disabled = index === boxes.length - 1;

      var panel = form.querySelector(".questions-panel");
      if (panel) {
        var top = panel.getBoundingClientRect().top + window.pageYOffset - 90;
        if (window.pageYOffset > top) window.scrollTo({ top: top, behavior: "smooth" });
      }
    }

    if (prevBtn) prevBtn.addEventListener("click", function () { showBox(current - 1); });
    if (nextBtn) nextBtn.addEventListener("click", function () { showBox(current + 1); });

    document.addEventListener("keydown", function (event) {
      if (event.target && /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName)) return;
      if (event.key === "ArrowLeft") { showBox(current - 1); }
      if (event.key === "ArrowRight") { showBox(current + 1); }
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
            showBox(i);
            unanswered.scrollIntoView({ behavior: "smooth", block: "center" });
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

    showBox(0);
  }

  /* ------------------------------------------------------- password toggle */

  function initPasswordToggles() {
    var buttons = document.querySelectorAll("[data-toggle-password]");
    Array.prototype.forEach.call(buttons, function (button) {
      button.addEventListener("click", function () {
        var field = document.getElementById(button.getAttribute("data-toggle-password"));
        if (!field) return;
        var hidden = field.type === "password";
        field.type = hidden ? "text" : "password";
        button.textContent = hidden ? "Hide" : "Show";
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

  /* ----------------------------------------------------------------- boot */

  function boot() {
    initQuiz();
    initPasswordToggles();
    initConfirmations();
    initValidation();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
