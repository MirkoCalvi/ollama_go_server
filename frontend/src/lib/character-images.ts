import august from "@/assets/august.png"
import frank from "@/assets/frank.png"
import james from "@/assets/james.png"
import olivia from "@/assets/olivia.png"
import robert from "@/assets/robert.png"

// Static name → image-URL map. Vite bundles each asset and returns a
// hashed public URL, so images aren't inlined into the JS bundle.
//
// If the backend ever registers a character that's not listed here,
// characterImageFor() returns null and the UI falls back to text-only.
const images: Record<string, string> = {
  August: august,
  Frank: frank,
  James: james,
  Olivia: olivia,
  Robert: robert,
}

export function characterImageFor(name: string): string | null {
  return images[name] ?? null
}
