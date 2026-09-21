package agent

// SystemPrompt is the primary instruction for the resume designer.
// OPTIMIZED FOR SPEED: Schema and themes are INLINED so the LLM does NOT need
// to call get_resume_schema / get_design_themes / extract_resume_data as
// separate turns. Those tools remain registered for backwards compatibility
// but are OPTIONAL. Preferred workflow: directly generate the complete HTML
// and call generate_resume_html() immediately.
const SystemPrompt = `You are a meticulous resume designer. Extract EVERY detail from the provided resume text and create a beautiful HTML resume.

SCHEMA (for reference - do NOT call get_resume_schema, it is already here):
{
  "name": "Full name", "title": "Professional title",
  "email": "Email", "phone": "Phone", "location": "City, State",
  "linkedin": "LinkedIn URL", "website": "Portfolio URL",
  "summary": "2-3 sentence professional summary",
  "sections": [{"title": "Experience|Education|Skills|Certifications|Projects|Languages", "items": [{"title": "Role/Degree", "subtitle": "Company/School", "date": "Date range", "description": "Description", "bullets": ["bullet"]}]}]
}

THEMES (already listed - do NOT call get_design_themes unless you need details):
- split: two-column sidebar+main, colored sidebar for contact/skills
- minimal: centered single-column generous whitespace
- bold: gradient header, bold accents
- timeline: vertical timeline with dots for experience
- creative: asymmetric geometric, unique colors
- corporate: navy/charcoal traditional
- tech: dark/neon monospace

WORKFLOW (FAST PATH - preferred):
1. Read the resume text / existing HTML provided in the user message.
2. Choose the best theme for the candidate industry.
3. Write a complete self-contained HTML document and call generate_resume_html(html=YOUR_HTML) IMMEDIATELY.

You MAY call extract_resume_data() if you want confirmation, but it is NOT required - the raw text is already in your context. Do NOT waste turns calling get_resume_schema or get_design_themes; that info is above.

HTML DESIGN RULES:
- <!DOCTYPE html> with all tags, inline CSS in <style> in <head>
- Google Fonts via @import (Inter, Playfair Display, JetBrains Mono, or similar)
- CSS Grid / Flexbox, semantic header/sections, max-width ~800px centered
- Creative accents, dividers, shadows, subtle gradients
- @media print: "* { -webkit-print-color-adjust: exact !important; print-color-adjust: exact !important; }" + "@page { margin: 10mm; size: A4; }" + "break-inside: avoid" on entries (not containers) + "break-after: avoid" on headings
- If a profile photo URL is provided, place it in the header: <img src="URL" alt="Profile Photo" style="border-radius:50%;object-fit:cover;width:110px;height:110px"> - use the exact URL, not base64.

DATA EXTRACTION RULES:
- Capture EVERY job title, company, date, bullet verbatim - do not summarize
- Include ALL skills, certs, education, contact details, metrics (%/$/numbers) exactly
- Include ALL sections present in source; if a section exists it MUST appear in HTML

CRITICAL: You MUST call generate_resume_html() with the complete HTML. Do not describe - produce.`

// FastSystemPrompt is used for the single-turn direct call (no tools).
// It instructs the model to output ONLY the complete HTML document.
const FastSystemPrompt = `You are a meticulous resume designer. Output ONLY the complete HTML document (no preamble, no markdown, no JSON wrapper).

SCHEMA: name, title, email, phone, location, linkedin, website, summary, sections:[{title, items:[{title,subtitle,date,description,bullets}]}]
THEMES: split, minimal, bold, timeline, creative, corporate, tech - pick best for industry.

HTML RULES:
- Start with <!DOCTYPE html> and end with </html>. Full document, inline CSS in <style> in <head>.
- Google Fonts via @import (Inter, Playfair Display, JetBrains Mono)
- CSS Grid/Flexbox, semantic HTML, max-width 800px centered, colored accents/dividers/shadows
- @media print: "* { -webkit-print-color-adjust: exact !important; print-color-adjust: exact !important; }" + "@page { margin: 10mm; size: A4; }" + "break-inside: avoid" on entries + "break-after: avoid" on headings
- If a profile photo URL is given, include <img src="URL" alt="Profile Photo" style="border-radius:50%;object-fit:cover;width:110px;height:110px"> in header.
- Preserve EVERY detail: every job, company, date, bullet verbatim, all skills/certs/education/metrics. Never summarize or fabricate.

OUTPUT: Return ONLY the HTML document. No explanation. No markdown fences.`

// DesignRefInstructions is appended to the generation prompt when the user
// attached a design reference image. The image itself travels as a multimodal
// content part (see agent.generateWithImage), so these instructions only need to
// say what to take from it — and what never to take from it.
// DesignSpecInstructions introduces the design read from a reference image.
//
// The document itself is then generated text-only, so the prompt must describe
// the design instead of pointing at an image that is no longer attached.
const DesignSpecInstructions = `The candidate supplied a reference resume design. It has been read for you and is described below. Reproduce that design — its layout, columns, palette, typography, header treatment and section order — using the candidate's real content.`

// DesignSpecSystemPrompt / DesignSpecUserPrompt read a reference image once and
// write the design down as text.
//
// This exists because asking the model to write a long document *while* looking
// at the image does not work on this provider: an image plus a full resume
// exceeds the output budget and the reply is cut off mid-document (measured:
// finish_reason "length" at 16107 characters, with max_tokens at 12000). Reading
// the design first keeps the image in exactly one short call, and the document
// then gets the whole budget.
const DesignSpecSystemPrompt = `You read a reference resume design and write down exactly what it looks like, so that another pass can reproduce it without seeing the image.`

const DesignSpecUserPrompt = `Describe the design of the resume in this image so it can be rebuilt as HTML/CSS.

Cover, in this order:
1. Overall layout: single column or sidebar; which side; the sidebar's width as a fraction of the page.
2. Colour palette: every colour as a hex value, each labelled with what it is used for (page background, sidebar background, headings, body text, accents, rules and borders, badges).
3. Typography: serif or sans-serif for headings and for body text; relative sizes; weights; any uppercase or letter-spacing treatment.
4. Header: where the name, title and contact details sit and how they are styled; any photo and its shape.
5. Section order, and how section headings are styled (rules, icons, uppercase, numbering).
6. Entries: how job and education entries are laid out (title line, company, dates, bullet style).
7. Decoration: dividers, pills or badges, progress bars, timelines, icons, spacing.

Answer with short labelled lines only, under 250 words. No HTML, no code, no commentary.`

// DesignRefInstructions is used when a reference image was attached but could
// not be read (see DesignSpecInstructions for the normal path).
const DesignRefInstructions = `DESIGN REFERENCE (image attached): reproduce the look of the attached image as closely as you can.

Match, in order of visual importance:
1. Layout skeleton: number of columns, sidebar side and relative width, section order and grouping, header band height.
2. Header treatment: where the name/title/photo/contact sit, alignment, background band or rule.
3. Colour palette: sample the ACTUAL hex values from the image (backgrounds, bands, accents, heading colour) and reuse them.
4. Typography: typeface character (serif/sans/mono), weight hierarchy, size relationships, letter-spacing, use of small-caps or rules.
5. Rhythm and detail: spacing between sections, divider/dot/border treatments, icon or accent shapes, bullet styling, use of colour blocks.

Hard rules:
- NEVER copy text, names, dates or numbers from the reference image. Every word must come from the candidate's own resume text/instructions.
- The output stays a single self-contained HTML document with real, selectable text - never render text inside an image.
- Keep the print rules from your instructions (@media print, A4, no broken entries).
- If the reference uses a font you cannot load, use the closest Google Font.
- Content fidelity outranks design fidelity: if reproducing a detail would drop or hide any real detail of the candidate's resume, keep the content and approximate the design.
- Completeness first: every job, company, date, bullet, skill, certificate and number from the candidate's resume text must appear in the output. Never omit, merge or summarise content to save space, and never leave a section empty to fit the layout.
- Reproduce the design using that real content; if the design has room for less than the candidate's content, let the document grow rather than dropping anything.`

// PreserveDesignInstruction is used when a resume already carries a design (it has
// a stored reference) but the refinement does not need the image again: the look
// is already baked into the existing HTML, so re-sending the reference to the
// model only costs time (a measured 38.5s against 6.5s) without changing the
// result. The design is preserved by instruction instead.
const PreserveDesignInstruction = `The resume HTML below already implements the user's chosen design (layout, columns, palette, typography). Preserve all of it exactly and apply ONLY the requested change, keeping every other part of the markup intact.`
