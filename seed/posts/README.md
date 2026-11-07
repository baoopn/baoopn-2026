# Seed posts

Each folder here is one blog post. On start the API imports every post it has
not handled before as a **draft** (settings key `seed.posts_2026` lists the
slugs already handled, so a post deleted in the admin is not imported again).
A slug that already exists in the database is skipped.

```
seed/posts/<slug>/
  post.json
  part-1.md
  part-2.md
  img/<id>.png        # or .jpg / .webp
```

`post.json`:

```json
{
  "slug": "my-post",
  "title": "My post",
  "excerpt": "",
  "category": "tech",
  "tags": ["go", "postgres"],
  "parts": [
    { "title": "Setup", "file": "part-1.md" },
    { "title": "", "file": "part-2.md" }
  ],
  "screenshots": [
    { "id": "dashboard" },
    { "id": "login", "file": "img/login-screen.jpg" }
  ]
}
```

- `slug` defaults to the folder name; `excerpt` empty = generated from part 1.
- `category` is `tech` (the default when missing) or `life`.
- `parts` (1–20) are saved in order; `file` is relative to the post folder and
  defaults to `part-<n>.md`. An empty part title shows as "Part <n>".
- In Markdown, `![alt](screenshot:<id>)` is replaced by the uploaded image's
  `/media/...` URL. The image is `img/<id>.png|jpg|webp`, or the screenshot's
  `file` (relative to the post folder). A missing image is dropped from the
  text with a warning in the log; it never stops the API from starting.
- A post whose `post.json` or part files cannot be read, or that fails
  validation, is skipped with a warning and retried on the next start.
