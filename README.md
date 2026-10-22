# Reading Trainer

A small web application for Myanmar reading practice. Learners read a
Myanmar text and then answer multiple choice questions about it. Everything
the learners see — the texts, the questions and the correct answers — is
managed by administrators in the browser.

* **Backend:** Go (standard library only, plus a pure Go SQLite driver)
* **Frontend:** HTML, CSS and vanilla JavaScript, rendered server side
* **Storage:** a single SQLite file

---

## What it does

| Step | Page | What happens |
| --- | --- | --- |
| 1 | `/login`, `/register` | A visitor signs in or creates a normal account. |
| 2 | `/` | The landing page shows one button per lesson. |
| 3 | `/lessons/{id}` | The **Myanmar language page**: the text of the lesson in a box and a **Next** button. |
| 4 | `/lessons/{id}/quiz` | The **question page**: the same text stays on screen on the left, the questions are on the right in boxes of five. Arrow buttons move between the boxes, the text does not move. The **Submit** button sits in the bottom right corner. |
| 5 | `/results/{id}` | The **result page**: the score such as `4 / 20`, the answers and a **Home** button back to the landing page. |

The number of questions per lesson is free. The quiz page always splits them
into boxes of five, so 20 questions become four boxes and the arrows move
through them.

---

## Account types and permissions

| Action | Learner | Administrator | Main administrator |
| --- | :---: | :---: | :---: |
| Register an account | ✔ (self service) | ✔ | ✔ |
| Read the lessons and take the quizzes | ✔ | ✔ | ✔ |
| See the own results | ✔ | ✔ | ✔ |
| See the results of everybody | | ✔ | ✔ |
| Delete results | | ✔ | ✔ |
| Create lessons and questions | | ✔ | ✔ |
| Change lessons and questions | | ✔ | ✔ |
| Delete lessons | | ✔ | ✔ |
| Create **normal** accounts | | ✔ | ✔ |
| Create **administrator** accounts | | | ✔ |
| Change the role of an account | | | ✔ |
| Delete a normal account | | ✔ | ✔ |
| Delete an administrator account | | ✔ (not the main one) | ✔ (not the main one) |
| Delete the main administrator | | | ✘ (never) |

The **main administrator** is the very first administrator account. It is
protected in the database with a unique index, so a second one can never be
created. It cannot be deleted, not even by itself.

Nobody can delete their own account — that would lock everybody out of the
site.

---

## Requirements

* Go 1.26 or newer (the Docker image ships its own toolchain)
* No C compiler and no other system dependency: the SQLite driver is pure Go

---

## Quick start

```bash
# 1. create the main administrator
go run ./cmd/adminctl create-admin --username root --password 'a-strong-password'

# 2. start the server
go run ./cmd/server
```

The site is now on <http://localhost:8080>. Sign in as `root` and open
**Administration → Lessons** to write the first lesson.

Both commands read `DB_PATH`, so they use the same database file
(`data/app.db` by default).

### Build instead of run

```bash
make build            # writes bin/server and bin/adminctl
./bin/server
```

---

## Creating the first administrator

There is no built-in default password — that would be a security hole in
every deployment. There are two supported ways:

### 1. The command line (recommended)

```bash
DB_PATH=/var/lib/reading-trainer/app.db \
  ./bin/adminctl create-admin --username root --password 'a-strong-password'
```

### 2. Environment variables (for containers and CI)

Set `ADMIN_USERNAME` and `ADMIN_PASSWORD` before the first start. The server
creates the main administrator **only when no main administrator exists
yet**, so restarting never touches an existing account:

```yaml
environment:
  ADMIN_USERNAME: root
  ADMIN_PASSWORD: a-strong-password
  ADMIN_DISPLAY_NAME: Root Administrator
```

### Other account operations

```bash
./bin/adminctl create-user --username alice --password 'secret'   # learner
./bin/adminctl create-user --username boss  --password 'secret' --role admin
./bin/adminctl passwd      --username alice --password 'new-secret'
./bin/adminctl promote     --username alice
./bin/adminctl demote      --username alice
./bin/adminctl list
```

`passwd` also ends every session of that account.

---

## Configuration

All configuration comes from environment variables. See `.env.example` for a
copyable template.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ADDR` | `:8080` | Listen address |
| `DB_PATH` | `data/app.db` | SQLite file, the directory is created automatically |
| `SESSION_TTL` | `168h` | Lifetime of a login |
| `SESSION_COOKIE_NAME` | `rt_session` | Name of the session cookie |
| `COOKIE_SECURE` | `false` | Set to `true` when the site runs on HTTPS |
| `BASE_URL` | — | Public base URL of the site |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `DEV_MODE` | `false` | Relaxes the content security policy for development |
| `ADMIN_USERNAME` | — | Bootstraps the main administrator |
| `ADMIN_PASSWORD` | — | Password of the bootstrapped administrator |
| `ADMIN_DISPLAY_NAME` | `Administrator` | Display name of the bootstrapped administrator |

---

## Deployment

### Docker

```bash
docker build -t reading-trainer:latest .
docker run -d --name reading-trainer \
  -p 8080:8080 \
  -v reading-trainer-data:/data \
  -e ADMIN_USERNAME=root \
  -e ADMIN_PASSWORD='a-strong-password' \
  --restart unless-stopped \
  reading-trainer:latest
```

or with the included compose file:

```bash
ADMIN_USERNAME=root ADMIN_PASSWORD='a-strong-password' docker compose up -d
```

The database lives in the `/data` volume, so the container can be replaced
without losing anything.

### Behind a reverse proxy (nginx) with HTTPS

```nginx
server {
    listen 443 ssl http2;
    server_name reading.example.com;

    ssl_certificate     /etc/letsencrypt/live/reading.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/reading.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

With HTTPS in front, set `COOKIE_SECURE=true` so the session cookie is only
sent over an encrypted connection.

### systemd

```ini
[Unit]
Description=Reading Trainer
After=network.target

[Service]
Type=simple
User=reading
WorkingDirectory=/opt/reading-trainer
ExecStart=/opt/reading-trainer/bin/server
Environment=ADDR=127.0.0.1:8080
Environment=DB_PATH=/var/lib/reading-trainer/app.db
Environment=COOKIE_SECURE=true
Environment=LOG_LEVEL=info
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/reading-trainer

[Install]
WantedBy=multi-user.target
```

Back up the database regularly, it holds everything:

```bash
sqlite3 /var/lib/reading-trainer/app.db ".backup '/backup/app-$(date +%F).db'"
```

---

## Security

The application implements the usual defences for a cookie based web app:

* **Passwords** are hashed with PBKDF2-HMAC-SHA256 (210 000 iterations, a
  random salt per account) using `crypto/pbkdf2` from the standard library.
* **Sessions** live in the database. The browser only holds a random
  256 bit token in an `HttpOnly`, `SameSite=Lax` cookie that is marked
  `Secure` when `COOKIE_SECURE=true`.
* **Session fixation** is prevented by rotating the session id after every
  successful login and after a password change.
* **CSRF**: every session carries a token that must be sent back in the
  `csrf_token` field of every form. Requests without a valid token are
  rejected with `403`.
* **Validation** happens on the server for every form; the browser checks are
  only a convenience.
* **The score is computed on the server.** The browser only sends the chosen
  options, it can never tell the server what is correct.
* **Escape by default**: all output goes through `html/template`, so user
  input is escaped.
* **Security headers**: `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Permissions-Policy` and a content security policy.
* **Password comparison** is constant time.

---

## Routes

| Method | Path | Who |
| --- | --- | --- |
| `GET` | `/login`, `/register` | everybody |
| `POST` | `/login`, `/register` | everybody |
| `POST` | `/logout` | signed in |
| `GET` | `/` | signed in |
| `GET` | `/lessons/{id}` | signed in |
| `GET` | `/lessons/{id}/quiz` | signed in |
| `POST` | `/lessons/{id}/submit` | signed in |
| `GET` | `/results`, `/results/{id}` | owner or administrator |
| `GET` | `/account` | signed in |
| `POST` | `/account/name`, `/account/password` | signed in |
| `GET` | `/admin`, `/admin/users`, `/admin/lessons`, `/admin/attempts` | administrator |
| `POST` | `/admin/users`, `/admin/users/{id}/…` | administrator, main administrator for roles |
| `POST` | `/admin/lessons`, `/admin/lessons/{id}/…` | administrator |
| `POST` | `/admin/questions/{id}/…` | administrator |
| `GET` | `/healthz` | everybody |

---

## Project structure

```
cmd/
  server/          the web server
  adminctl/        account management from the command line
internal/
  auth/            password hashing, random tokens
  config/          configuration from the environment
  middleware/      recover, logger, security headers, sessions, CSRF, guards
  store/           SQLite data access (users, sessions, lessons, questions, attempts)
  validate/        input validation
  web/             handlers, routing and the HTML templates
    templates/     one file per page, all sharing the layout
    static/        style.css and app.js
```

The templates are embedded into the binary, so a deployment is a single
file plus the database.

---

## Tests

```bash
make test        # go test ./... -count=1
make test-race   # with the race detector
```

The suite covers the password hashing, the validation rules, the data layer
and the complete web flow: registering, signing in, the permissions of the
different account types, writing a lesson, taking a quiz, the scoring and
the deletion rules.
