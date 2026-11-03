# Vanguard Blog

This folder contains the Vanguard blog. The blog is rendered using
[Hugo](https://gohugo.io/): `layouts/index.html` renders the post list,
`layouts/posts/single.html` renders each post.

During deploy, the pages workflow renders this folder into HTML and serves it
at `/blog/`. The rendered pages reuse the main site's stylesheet
(`website/static/main.css`) and top bar, so they look identical to the rest of
the site.

## Writing a new blog post

Posts are written using Markdown. The name of the file corresponds to the URL
slug. The file name must only contain these regex characters `+\w-`.

```bash
$ hugo new posts/name-of-new-post.md
$ <your-editor> content/posts/name-of-new-post.md
```

This generates something like:

```markdown
---
title: "Name of New Post"
date: 2022-11-28T16:39:38+11:00
draft: true
author: Your Name
---
First paragraph.
<!--more-->

Blah blah.
```

The frontmatter section contains metadata about the post, such as the title and
your name. The `<!--more-->` marker is used to
[split summaries](https://gohugo.io/content-management/summaries/#manual-summary-splitting).

If `draft` is set to `true`, the post is not rendered or included by default.

## Testing changes

[Install Hugo](https://gohugo.io/installation/), then run:

```bash
$ hugo --source website/blog server
```

and navigate to <http://localhost:1313/blog/>.
