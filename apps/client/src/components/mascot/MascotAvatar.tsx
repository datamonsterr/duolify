import { Mascot, type MascotProps } from "./Mascot";

/** Round chat avatar: the bust, crisp from 24 px. Decorative unless `label` is given. */
export function MascotAvatar({ size = 40, ...rest }: MascotProps) {
  return (
    <span className="m-avatar" style={{ width: size, height: size }}>
      <Mascot {...rest} view="bust" size={size * 0.9} />
    </span>
  );
}
