/** Saves text as a file in the browser's downloads. */
export function saveFile(text: string, name: string, type: string) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([text], { type }));
  a.download = name;
  a.click();
  setTimeout(() => {
    URL.revokeObjectURL(a.href);
  }, 1000);
}
