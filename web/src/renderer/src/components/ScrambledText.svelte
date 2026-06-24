<!-- $lib/components/ScrambledText.svelte -->
<script lang="ts">
  import { animate, scrambleText } from "animejs";

  let {
    text,
    active = true,
    maskChar = "•",
    class: className = "",
  }: {
    text: string;
    active?: boolean;
    maskChar?: string;
    chars?: string;
    duration?: number;
    loopDelay?: number;
    class?: string;
  } = $props();

  // Never hand the real text to the animation — only its "shape" survives.
  function mask(value: string): string {
    return value.replace(/\S/g, maskChar);
  }

  let el: HTMLElement | undefined = $state();

  $effect(() => {
    if (!el || !active || !text) return () => {};

    const animation = animate(el, {
      innerHTML: scrambleText({ text: mask(text) }),
      override: true,
      revealDelay: 2000,
    });

    // Stops the animation when `active` flips off, `text` changes,
    // or this component unmounts.
    return () => animation.cancel();
  });
</script>

{#if text}
  {#if active}
    <span bind:this={el} class="font-mono {className}">{mask(text)}</span>
  {:else}
    <span class={className}>{text}</span>
  {/if}
{/if}
