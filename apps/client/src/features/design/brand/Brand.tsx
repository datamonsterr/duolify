import { Link } from "@tanstack/react-router";
import "./brand.css";

export interface BrandMarkProps {
  size?: number | string;
  className?: string;
  decorative?: boolean;
  label?: string;
}

export function BrandMark({
  size = 32,
  className = "",
  decorative = true,
  label = "Duolify",
}: BrandMarkProps) {
  const pixelSize = typeof size === "number" ? `${size}px` : size;

  return (
    <svg
      className={`brand-mark brand-mark-svg ${className}`.trim()}
      data-brand-mark="true"
      viewBox="0 0 32 32"
      width={pixelSize}
      height={pixelSize}
      role={decorative ? "presentation" : "img"}
      aria-hidden={decorative ? "true" : undefined}
      aria-label={decorative ? undefined : label}
      style={{
        width: pixelSize,
        height: pixelSize,
        flexShrink: 0,
        display: "inline-block",
        verticalAlign: "middle",
      }}
    >
      {!decorative && <title>{label}</title>}
      {/* Tile 1: Lime green rounded tile (#58C322) */}
      <rect
        data-tile="lime"
        x="2"
        y="8"
        width="20"
        height="20"
        rx="6"
        fill="var(--lime, #58C322)"
      />
      {/* Tile 2: Sky blue offset rounded tile (#1677C9) */}
      <rect
        data-tile="secondary"
        x="10"
        y="2"
        width="20"
        height="20"
        rx="6"
        fill="var(--secondary, #1677C9)"
      />
    </svg>
  );
}

export interface BrandWordmarkProps {
  className?: string;
}

export function BrandWordmark({ className = "" }: BrandWordmarkProps) {
  return (
    <span className={`brand-wordmark ${className}`} data-brand-wordmark="true">
      duolify
    </span>
  );
}

export interface BrandProps {
  href?: string;
  to?: string;
  size?: number | string;
  className?: string;
}

export function Brand({ href, to, size = 32, className = "" }: BrandProps) {
  const content = (
    <>
      <BrandMark size={size} decorative />
      <BrandWordmark />
    </>
  );

  if (to) {
    return (
      <Link to={to} className={`brand ${className}`} data-brand="true">
        {content}
      </Link>
    );
  }

  return (
    <a href={href || "/"} className={`brand ${className}`} data-brand="true">
      {content}
    </a>
  );
}
