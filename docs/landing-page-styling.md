# Landing page styling notes (Docsy gotchas)

Reference for anyone editing `docs/site/content/_index.md` — the landing
page uses Docsy's `blocks/*` shortcodes, which have two non-obvious
behaviors that bit us.

## Two gotchas to remember

### 1. `blocks/section` needs an explicit `color`

The shortcode template (`layouts/_shortcodes/blocks/section.html` in
the Docsy module) builds its background class like this:

```go-html-template
{{ $col_id := .Get "color" | default .Ordinal -}}
<section class="row td-box td-box--{{ $col_id }} ...">
```

If you omit `color`, it falls back to the section's ordinal position
on the page (0, 1, 2, ...). That number is mapped to a palette entry
in Docsy's `$td-box-colors`:

```scss
$td-box-colors:
  $dark, $primary, $secondary, $info, $white, $gray-600, $success, ...;
```

So an unannotated 4th section silently gets `td-box--3`, which paints
it `$info` blue. Always set `color` explicitly (`"white"`, `"primary"`,
`"dark"`, etc.) — otherwise reordering sections changes their colors.

### 2. Code blocks need a `td-content` wrapper

All of Docsy's code-block styling (`pre` background, border, padding,
`.highlight` margins, syntax-token colors) is scoped under
`.td-content` in `assets/scss/td/_code.scss`:

```scss
.td-content {
  .highlight { ... }
  pre { background-color: var(--td-pre-bg); border: ... }
  .chroma { ... }
}
```

That class wraps regular documentation pages but **not** the landing
page sections. So a markdown code fence inside `{{% blocks/section %}}`
renders as a perfectly correct `<pre><code class="chroma">` with all
the right Chroma spans — and zero styling. It looks like unformatted
plain text.

The fix is to wrap the section's inner content in a `td-content` div.
While you're at it, constrain the width with a Bootstrap column so
the prose doesn't stretch edge-to-edge.

## The pattern

```markdown
{{% blocks/section color="white" %}}

<div class="td-content col-12 col-lg-8 mx-auto">

## Install

Prose, `inline code`, links — all render normally because we're inside
`td-content` now.

```bash
URL=https://github.com/gke-demos/workload-resizer/releases/latest/download
kubectl apply -f $URL/config.yaml
kubectl apply -f $URL/install.yaml
```

The [Install guide](docs/install/) link styling also inherits from
`td-content`.

</div>

{{% /blocks/section %}}
```

The blank lines around `<div>` and `</div>` are required — without
them Goldmark treats the whole block as raw HTML and stops processing
markdown inside it.

## Previewing locally

The project doesn't bundle Hugo. You need the **extended** build for
Docsy's SCSS pipeline (libsass). Pure-Go `hugo` will fail with:

> Check your Hugo installation; you need the extended version to build
> SCSS/SASS with transpiler set to 'libsass'.

Quick fetch (Linux x86_64):

```bash
curl -sL https://github.com/gohugoio/hugo/releases/download/v0.161.1/hugo_extended_0.161.1_linux-amd64.tar.gz \
  | tar xz hugo -C /tmp
cd docs/site
npm install                 # PostCSS + autoprefixer for the SCSS pipeline
/tmp/hugo server --bind 0.0.0.0 --port 1313 --disableFastRender
```

Then open <http://localhost:1313/workload-resizer/>. Edits to
`_index.md` hot-reload.
