export function composeHtmlText(html: string): string {
  // Template contents stay inert: importing or saving an HTML draft must not
  // activate its images, stylesheets or other remote resources.
  const template = document.createElement("template");
  template.innerHTML = html;
  template.content.querySelectorAll("script, style, link, title").forEach((element) => element.remove());
  template.content.querySelectorAll("br").forEach((element) => element.replaceWith("\n"));
  template.content.querySelectorAll("p, div, blockquote, pre, li, h1, h2, h3, h4, h5, h6, tr").forEach((element) => element.append("\n"));
  template.content.querySelectorAll("td, th").forEach((element) => element.append("\t"));
  return (template.content.textContent ?? "").replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim();
}
