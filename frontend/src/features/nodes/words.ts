/** "Ready", "Not ready", "Memory pressure", "No PID pressure"… */
export function conditionWord(type: string, status: string): string {
  if (type === "Ready") return status === "True" ? "Ready" : "Not ready";
  const words = type
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .split(" ")
    // Keep acronyms such as PID as they are.
    .map((w) => (w.length > 1 && w === w.toUpperCase() ? w : w.toLowerCase()))
    .join(" ");
  return status === "True" ? words.charAt(0).toUpperCase() + words.slice(1) : "No " + words;
}
