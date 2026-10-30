import { expect, it } from "vitest";
import { composeHtmlText } from "@/features/compose/html-text";

it("extracts decoded text with paragraph, line and table-cell boundaries", () => {
  expect(composeHtmlText("<p>A &amp; B</p><div>Second<br>line</div><table><tr><td>one</td><td>two</td></tr></table>"))
    .toBe("A & B\nSecond\nline\none\ttwo");
});

it("does not include document styles or scripts in a plain-text alternative", () => {
  expect(composeHtmlText('<style>body{display:none}</style><script>unsafe()</script><p>Body</p><img src="https://example.invalid/pixel">'))
    .toBe("Body");
});
