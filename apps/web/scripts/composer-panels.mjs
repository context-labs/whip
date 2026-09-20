import assert from 'node:assert/strict';

// Check the actual production surfaces, including empty queue live-region wrappers.
export async function checkComposerPanels(form) {
  const geometry = await form.evaluate(element => {
    const input = element.querySelector('[data-whip-composer]');
    const box = input.parentElement;
    const dock = element.querySelector('[data-agent-dock]');
    const queue = element.querySelector('[data-composer-queue]');
    const panels = [dock, queue].filter(panel => panel && panel.getBoundingClientRect().height > 0);
    const rect = node => { const r = node.getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, height: r.height }; };
    return { composer: rect(box), viewport: innerHeight, panels: panels.map((panel, index) => {
      const style = getComputedStyle(panel);
      const rows = panel.querySelector('[data-agent-dock-rows], ol');
      const content = rows ?? panel.querySelector('[data-agent-dock-content]');
      return { kind: panel.hasAttribute('data-agent-dock') ? 'agents' : 'queue', ...rect(panel),
        next: rect(panels[index + 1] ?? box), content: content && rect(content),
        border: style.borderTopWidth, radius: style.borderTopLeftRadius, background: style.backgroundColor,
        scrollHeight: rows?.scrollHeight, scrollViewport: rows?.clientHeight };
    }) };
  });
  for (const panel of geometry.panels) {
    assert.equal(panel.border, '1px');
    assert.equal(panel.radius, '20px');
    assert.ok(Math.abs(panel.left - geometry.composer.left - 8) < 1, `${panel.kind}: one inset, no duplicate gutters`);
    assert.ok(Math.abs(geometry.composer.right - panel.right - 8) < 1, `${panel.kind}: symmetric inset`);
    assert.ok(Math.abs(panel.bottom - panel.next.top - 12) < 1, `${panel.kind}: only the 12px empty tail overlaps`);
    if (panel.content) {
      assert.ok(panel.content.bottom <= panel.next.top + 1, `${panel.kind}: final content clears the next surface`);
      assert.ok(Math.abs(panel.content.bottom - panel.next.top) < 1, `${panel.kind}: no empty band above the next surface`);
    }
  }
  if (geometry.panels.length === 2) {
    assert.deepEqual(geometry.panels.map(panel => panel.kind), ['agents', 'queue']);
    assert.equal(geometry.panels[0].background, geometry.panels[1].background);
  }
  return geometry;
}
