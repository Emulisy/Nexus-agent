/**
 * Shared GSAP motion presets for page entry, list items, message bubbles,
 * collapsible panels, and validation feedback. Each helper returns the animation
 * so callers can kill it or register it with a gsap.context cleanup. Reused
 * elements use overwrite to avoid competing animations on the same properties.
 */
import gsap from 'gsap';

/**
 * Fade and lift a page container. Pathname-driven callers can reuse the same
 * element; overwrite prevents the previous entry tween from competing.
 */
export function pageEnter(el: Element) {
  return gsap.fromTo(
    el,
    { opacity: 0, y: 14 },
    { opacity: 1, y: 0, duration: 0.45, ease: 'power3.out', overwrite: 'auto' },
  );
}

/**
 * Animate sibling items with a shared easing and configurable delay between
 * starts. The caller owns the returned tween or its surrounding GSAP context.
 */
export function staggerEnter(els: Element[] | NodeListOf<Element>, step = 0.05) {
  return gsap.fromTo(
    els,
    { opacity: 0, y: 10 },
    {
      opacity: 1,
      y: 0,
      duration: 0.4,
      ease: 'power3.out',
      stagger: step,
      overwrite: 'auto',
    },
  );
}

/**
 * Animate a newly mounted message bubble. Streaming updates reuse the component
 * and should not replay this animation for every incoming text fragment.
 */
export function bubbleIn(el: Element) {
  return gsap.fromTo(
    el,
    { opacity: 0, y: 12, scale: 0.98 },
    { opacity: 1, y: 0, scale: 1, duration: 0.35, ease: 'power3.out', overwrite: 'auto' },
  );
}

/**
 * Animate between collapsed height and measured content height. The caller
 * controls the open state and retains overflow clipping around the animated body.
 */
export function expandToggle(el: Element, open: boolean) {
  if (open) {
    return gsap.fromTo(
      el,
      { height: 0, opacity: 0 },
      { height: 'auto', opacity: 1, duration: 0.3, ease: 'power3.out', overwrite: 'auto' },
    );
  }
  return gsap.to(el, { height: 0, opacity: 0, duration: 0.25, ease: 'power2.in', overwrite: 'auto' });
}

/**
 * Brief horizontal validation feedback; callers keep the form and error text
 * responsible for communicating the actual validation failure.
 */
export function shake(el: Element) {
  return gsap.fromTo(
    el,
    { x: -6 },
    { x: 0, duration: 0.4, ease: 'elastic.out(1, 0.3)', overwrite: 'auto' },
  );
}

export { gsap };
