// Placeholder guide avatar until the mascot component lands (conversational-flows-and-mascot
// group 5). Same prop surface as the planned component; replace the body, not the callers.
export function MascotAvatar({ size = 32 }: { size?: number }) {
  return (
    <span
      className="guided-avatar"
      aria-hidden="true"
      style={{ inlineSize: size, blockSize: size }}
    />
  );
}
