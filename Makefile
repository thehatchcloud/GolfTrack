.DEFAULT_GOAL := help

.PHONY: help install migrate dev shell test lint css collectstatic clean distclean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Create .venv with Python 3.14 and install all dependencies
	uv venv --python 3.14 .venv
	uv pip install -e ".[dev]"

migrate: ## Apply Django migrations (creates db.sqlite3 if it does not exist)
	python manage.py migrate

dev: ## Start the development server at http://localhost:8000
	python manage.py runserver

shell: ## Open the Django interactive shell
	python manage.py shell

test: ## Run the full pytest suite
	.venv/bin/pytest -q

lint: ## Lint with ruff
	.venv/bin/ruff check .

css: ## Compile Tailwind CSS → static/css/app.css
	sh ./bin/build-css.sh

collectstatic: ## Collect static files for production
	DJANGO_DEBUG=false python manage.py collectstatic --noinput

clean: ## Remove generated artifacts (SQLite db, compiled CSS, __pycache__, staticfiles)
	rm -f db.sqlite3
	rm -f static/css/app.css
	rm -rf staticfiles/
	find . \( -path './.venv' -o -path './node_modules' -o -path './.next' \) -prune \
		-o -type d -name '__pycache__' -exec rm -rf {} +
	rm -rf .pytest_cache/

distclean: clean ## clean + remove the downloaded Tailwind binary and .venv
	rm -f bin/tailwindcss
	rm -rf .venv/
