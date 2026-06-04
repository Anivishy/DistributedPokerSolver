import React, { useState, useEffect, useCallback, useRef, memo } from 'react';


const RANKS = ['A', 'K', 'Q', 'J', 'T', '9', '8', '7', '6', '5', '4', '3', '2'];
const SUITS = ['s', 'h', 'd', 'c'];
const SUIT_SYMBOLS = { s: '♠', h: '♥', d: '♦', c: '♣' };
const SUIT_COLORS = { s: '#e6e6e6', h: '#e63946', d: '#4a9eff', c: '#26c281' };
const POSITIONS = ['UTG', 'MP', 'CO', 'BTN', 'SB', 'BB'];
const STREETS = ['Preflop', 'Flop', 'Turn', 'River'];

const BET_NAMES = new Set(['Bet 33%', 'Bet 50%', 'Bet 75%', 'Bet Pot', 'All-in']);
const BET_KEYS  = new Set(['bet33', 'bet50', 'bet75', 'betpot', 'allin']);
const ALL_ACTIONS = [
  { key: 'fold', label: 'Fold' },    { key: 'check', label: 'Check' },
  { key: 'call', label: 'Call' },    { key: 'bet33', label: 'Bet 33%' },
  { key: 'bet50', label: 'Bet 50%' },{ key: 'bet75', label: 'Bet 75%' },
  { key: 'betpot', label: 'Bet Pot' },{ key: 'allin', label: 'All-in' },
];

const PREFLOP_LABELS = {
  'Bet 33%': 'Raise 2bb', 'Bet 50%': 'Raise 2.5bb',
  'Bet 75%': 'Raise 3bb', 'Bet Pot': 'Raise 4bb',
};
const ACTION_NAME_TO_KEY = {
  'Fold': 'fold', 'Check': 'check', 'Call': 'call',
  'Bet 33%': 'bet33', 'Bet 50%': 'bet50', 'Bet 75%': 'bet75',
  'Bet Pot': 'betpot', 'All-in': 'allin',
};

function actionDisplayLabel(label, street) {
  if (street === 0 && PREFLOP_LABELS[label]) return PREFLOP_LABELS[label];
  return label;
}

function getPotAdd(actionKey, street, pot, lastBet, stack) {
  if (actionKey === 'fold' || actionKey === 'check') return 0;
  if (actionKey === 'call') return lastBet;
  if (actionKey === 'allin') return stack;
  if (street === 0) {
    const bb = { bet33: 2, bet50: 2.5, bet75: 3, betpot: 4 };
    return bb[actionKey] ?? 0;
  }
  const frac = { bet33: 0.33, bet50: 0.5, bet75: 0.75, betpot: 1.0 }[actionKey];
  return frac !== undefined ? Math.min(pot * frac, stack) : 0;
}


function fmtBB(x) {
  const r = Math.round(x * 10) / 10;
  return Number.isInteger(r) ? `${r}` : r.toFixed(1);
}

function actionAmount(actionKey, ctx) {
  const { street, pot, lastBet, stack, facingBet } = ctx;
  if (actionKey === 'fold' || actionKey === 'check') return 0;
  if (actionKey === 'call') return Math.min(lastBet, stack);
  if (actionKey === 'allin') return stack;
  if (!facingBet) {
    return Math.min(getPotAdd(actionKey, street, pot, lastBet, stack), stack);
  }
  if (street === 0) {
    return Math.min(getPotAdd(actionKey, 0, pot, lastBet, stack), stack);
  }
  const frac = { bet33: 0.33, bet50: 0.5, bet75: 0.75, betpot: 1.0 }[actionKey] || 0;
  return Math.min(lastBet + frac * (pot + lastBet), stack);
}

function actionLegal(actionKey, ctx) {
  const { lastBet, stack, facingBet } = ctx;
  if (actionKey === 'fold')  return facingBet;
  if (actionKey === 'check') return !facingBet;
  if (actionKey === 'call')  return facingBet && lastBet > 0;
  if (actionKey === 'allin') return stack > 0;
  const amt = actionAmount(actionKey, ctx);
  if (amt >= stack) return false;
  if (!facingBet)  return amt > 0;
  return amt > lastBet;
}

function actionLabel(actionKey, ctx) {
  const { street, facingBet, lastBet, stack } = ctx;
  if (actionKey === 'fold')  return 'Fold';
  if (actionKey === 'check') return 'Check';
  if (actionKey === 'call')  return `Call ${fmtBB(Math.min(lastBet, stack))}bb`;
  if (actionKey === 'allin') return `All-in ${fmtBB(stack)}bb`;
  const amt = actionAmount(actionKey, ctx);
  if (facingBet || street === 0) return `Raise to ${fmtBB(amt)}bb`;
  return `Bet ${fmtBB(amt)}bb`;
}

function strategyActionLabel(name, ctx) {
  const key = ACTION_NAME_TO_KEY[name] ?? name.toLowerCase();
  return actionLabel(key, ctx);
}

const API = '/api';


function allCards() {
  const cards = [];
  for (const r of RANKS) {
    for (const s of SUITS) {
      cards.push(r + s);
    }
  }
  return cards;
}

function rankValue(r) {
  const m = { '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8,
              '9': 9, 'T': 10, 'J': 11, 'Q': 12, 'K': 13, 'A': 14 };
  return m[r] || 0;
}


function buildHeatmapGrid(rangeWeights) {
  const grid = Array.from({ length: 13 }, () => Array(13).fill(0));
  const counts = Array.from({ length: 13 }, () => Array(13).fill(0));

  let comboIdx = 0;
  for (let i = 0; i < 52; i++) {
    const rank1 = Math.floor(i / 4) + 2;
    const suit1 = i % 4;
    for (let j = i + 1; j < 52; j++) {
      const rank2 = Math.floor(j / 4) + 2;
      const suit2 = j % 4;

      const highRank = Math.max(rank1, rank2);
      const lowRank = Math.min(rank1, rank2);
      const ri = 14 - highRank;
      const ci = 14 - lowRank;

      let row, col;
      if (rank1 === rank2) {
        row = ri; col = ri;
      } else if (suit1 === suit2) {
        row = ri; col = ci;
      } else {
        row = ci; col = ri;
      }

      const w = (rangeWeights && rangeWeights[comboIdx]) || 0;
      grid[row][col] += w;
      counts[row][col]++;
      comboIdx++;
    }
  }

  for (let r = 0; r < 13; r++) {
    for (let c = 0; c < 13; c++) {
      if (counts[r][c] > 0) {
        grid[r][c] /= counts[r][c];
      }
    }
  }
  return grid;
}

function RangeHeatmap({ rangeWeights }) {
  const grid = buildHeatmapGrid(rangeWeights);
  const maxVal = grid.flat().reduce((a, b) => Math.max(a, b), 0.001);

  return (
    <div style={{ overflowX: 'auto' }}>
      <table style={{ borderCollapse: 'collapse', fontSize: 11 }}>
        <thead>
          <tr>
            <th style={{ width: 18 }} />
            {RANKS.map(r => (
              <th key={r} style={{ width: 28, textAlign: 'center', color: '#888', fontWeight: 600 }}>{r}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {RANKS.map((rowRank, ri) => (
            <tr key={rowRank}>
              <td style={{ color: '#888', fontWeight: 600, paddingRight: 4, textAlign: 'right' }}>{rowRank}</td>
              {RANKS.map((colRank, ci) => {
                const w = grid[ri][ci];
                const intensity = Math.min(w / maxVal, 1);
                const alpha = 0.08 + intensity * 0.92;
                let bg = `rgba(100,180,255,${alpha})`;
                if (ri === ci) bg = `rgba(255,200,80,${alpha})`;
                else if (ri > ci) bg = `rgba(120,220,120,${alpha})`;
                const label = ri === ci
                  ? `${rowRank}${rowRank}`
                  : ri < ci
                    ? `${rowRank}${colRank}s`
                    : `${rowRank}${colRank}o`;
                return (
                  <td
                    key={colRank}
                    title={`${label}: ${(w * 100).toFixed(2)}%`}
                    style={{
                      width: 28, height: 22, background: bg,
                      border: '1px solid #333', textAlign: 'center',
                      fontSize: 9, color: intensity > 0.6 ? '#111' : '#ccc',
                      cursor: 'default',
                    }}
                  >
                    {label}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}


function PlayingCard({ card, size = 'md', selected, blocked, onClick }) {
  if (!card) {
    const sz = { sm: 32, md: 48, lg: 64 }[size];
    return (
      <div
        onClick={onClick}
        style={{
          width: sz, height: sz * 1.4, border: '2px dashed #555',
          borderRadius: 6, cursor: onClick ? 'pointer' : 'default',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#555', fontSize: sz * 0.3,
        }}
      >
        +
      </div>
    );
  }
  const rank = card.slice(0, -1);
  const suit = card.slice(-1);
  const color = SUIT_COLORS[suit] || '#000';
  const sz = { sm: 32, md: 48, lg: 64 }[size];
  return (
    <div
      onClick={onClick}
      style={{
        width: sz, height: sz * 1.4,
        background: blocked ? '#333' : selected ? '#2a4a7f' : '#1e1e2e',
        border: `2px solid ${selected ? '#4a9eff' : blocked ? '#444' : '#555'}`,
        borderRadius: 6, cursor: onClick ? 'pointer' : 'default',
        display: 'flex', flexDirection: 'column',
        alignItems: 'center', justifyContent: 'center',
        color: blocked ? '#555' : color,
        opacity: blocked ? 0.4 : 1,
        userSelect: 'none',
      }}
    >
      <span style={{ fontSize: sz * 0.35, fontWeight: 700, lineHeight: 1 }}>{rank}</span>
      <span style={{ fontSize: sz * 0.4, lineHeight: 1 }}>{SUIT_SYMBOLS[suit]}</span>
    </div>
  );
}

const CardPicker = memo(function CardPicker({ usedCards, onSelect }) {
  return (
    <div style={{
      display: 'grid', gridTemplateColumns: 'repeat(13, 1fr)',
      gap: 3, padding: 12, background: '#12121e', borderRadius: 8,
      border: '1px solid #333',
    }}>
      {RANKS.map(rank =>
        SUITS.map(suit => {
          const card = rank + suit;
          const blocked = usedCards.includes(card);
          return (
            <PlayingCard
              key={card} card={card} size="sm"
              blocked={blocked}
              onClick={!blocked ? () => onSelect(card) : undefined}
            />
          );
        })
      )}
    </div>
  );
});

function StrategyBar({ action, label, frequency, ev }) {
  const pct = Math.round(frequency * 100);
  const ACTION_COLORS = {
    Fold: '#e63946', Check: '#4a9eff', Call: '#06d6a0',
    'Bet 33%': '#f4a261', 'Bet 50%': '#e9c46a',
    'Bet 75%': '#e76f51', 'Bet Pot': '#8338ec', 'All-in': '#ff006e',
  };
  const color = ACTION_COLORS[action] || '#888';
  return (
    <div style={{ marginBottom: 6 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 2 }}>
        <span style={{ color }}>{label ?? action}</span>
        <span style={{ color: '#aaa' }}>{pct}%{ev !== undefined ? ` (EV: ${ev})` : ''}</span>
      </div>
      <div style={{ height: 8, background: '#1e1e2e', borderRadius: 4, overflow: 'hidden' }}>
        <div style={{ width: `${pct}%`, height: '100%', background: color, borderRadius: 4, transition: 'width 0.3s' }} />
      </div>
    </div>
  );
}

function SolveProgress() {
  const [progress, setProgress] = useState(null);

  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const res = await fetch(`${API}/progress`);
        const data = await res.json();
        setProgress(data);
        if (data.pct >= 100) clearInterval(interval);
      } catch {  }
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  if (!progress || progress.pct >= 100) return null;

  return (
    <div style={{
      position: 'fixed', top: 0, left: 0, right: 0, zIndex: 1000,
      background: '#0d0d1a', borderBottom: '1px solid #333', padding: '8px 16px',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <span style={{ color: '#aaa', fontSize: 13 }}>
          Pre-solving GTO strategy… {progress.pct}% ({progress.solved}/{progress.total} iterations)
        </span>
        <div style={{ flex: 1, height: 4, background: '#1e1e2e', borderRadius: 2 }}>
          <div style={{ width: `${progress.pct}%`, height: '100%', background: '#4a9eff', borderRadius: 2, transition: 'width 0.5s' }} />
        </div>
      </div>
    </div>
  );
}


export default function App() {
  const [holeCards, setHoleCards] = useState([null, null]);
  const [boardCards, setBoardCards] = useState([null, null, null, null, null]);
  const [position, setPosition] = useState(3);
  const [oppPosition, setOppPosition] = useState(5);
  const [potSize, setPotSize] = useState(1.5);
  const [lastBetSize, setLastBetSize] = useState(1);
  const [stackSize, setStackSize] = useState(100);
  const [street, setStreet] = useState(0);

  const [pickingSlot, setPickingSlot] = useState(null);
  const [activeTab, setActiveTab] = useState('gto');
  const [loading, setLoading] = useState(false);
  const [brLoading, setBrLoading] = useState(false);
  const [rangeLoading, setRangeLoading] = useState(false);
  const [heroTurn, setHeroTurn] = useState(true);
  const [heroAction, setHeroAction] = useState(null);
  const [lastVillainAction, setLastVillainAction] = useState(null);
  const [handOver, setHandOver] = useState(false);

  const [gtoData, setGtoData] = useState(null);
  const [rangeWeights, setRangeWeights] = useState(null);
  const [rangeStats, setRangeStats] = useState(null);
  const [brData, setBrData] = useState(null);
  const [deviation, setDeviation] = useState(null);
  const [actionHistory, setActionHistory] = useState([]);

  const streetRef = useRef(0);
  useEffect(() => { streetRef.current = street; }, [street]);

  const gtoDebounce = useRef(null);

  const usedCards = [...holeCards, ...boardCards].filter(Boolean);


  const resetHand = useCallback(() => {
    setHoleCards([null, null]);
    setBoardCards([null, null, null, null, null]);
    setStreet(0);
    setActionHistory([]);
    setGtoData(null);
    setRangeWeights(null);
    setRangeStats(null);
    setBrData(null);
    setDeviation(null);
    setHeroTurn(position < oppPosition);
    setHeroAction(null);
    setLastVillainAction(null);
    setHandOver(false);
    setPickingSlot(null);
    setActiveTab('gto');
    setPotSize(1.5);
    setLastBetSize(1);
  }, [position, oppPosition]);

  const doAdvanceStreet = useCallback(() => {
    const s = streetRef.current;
    if (s >= 3) return;
    setStreet(s + 1);
    setActionHistory(prev => [...prev, { divider: true, street: s + 1 }]);
    setLastVillainAction(null);
    setLastBetSize(0);
  }, []);


  const fetchGTO = useCallback(async (h, b, pos, pot, stk, str) => {
    if (h.filter(Boolean).length < 2) return;
    setLoading(true);
    try {
      const res = await fetch(`${API}/gto`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          hole_cards: h.filter(Boolean),
          board_cards: b.filter(Boolean),
          position: pos,
          pot_size: pot,
          stack_size: stk,
          street: str,
        }),
      });
      const data = await res.json();
      setGtoData(data);
    } catch (err) {
      console.error('GTO fetch failed:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  const scheduleFetchGTO = useCallback((h, b, pos, pot, stk, str) => {
    clearTimeout(gtoDebounce.current);
    gtoDebounce.current = setTimeout(() => fetchGTO(h, b, pos, pot, stk, str), 300);
  }, [fetchGTO]);

  useEffect(() => {
    scheduleFetchGTO(holeCards, boardCards, position, potSize, stackSize, street);
  }, [holeCards, boardCards, position, potSize, stackSize, street, scheduleFetchGTO]);

  useEffect(() => {
    setHeroTurn(position < oppPosition);
    setHeroAction(null);
    setLastVillainAction(null);
    setHandOver(false);
  }, [street, position, oppPosition]);

  const observeAction = useCallback(async (actionKey, facingBet = false) => {
    setRangeLoading(true);
    const capturedHeroAction = heroAction;
    const currentWeights = rangeWeights || [];
    const ctx = { street, pot: potSize, lastBet: lastBetSize, stack: stackSize, facingBet };
    const amt = actionAmount(actionKey, ctx);
    try {
      const res = await fetch(`${API}/range/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          hole_cards: holeCards.filter(Boolean),
          board_cards: boardCards.filter(Boolean),
          action_type: actionKey,
          weights: currentWeights,
          position: oppPosition,
          pot_size: potSize,
          stack_size: stackSize,
          street,
        }),
      });
      const data = await res.json();
      setRangeWeights(data.weights);
      setRangeStats(data.stats);
      setLastVillainAction(actionKey);
      setActionHistory(prev => [...prev, { action: actionKey, street }]);

      if (amt > 0) {
        setPotSize(prev => prev + amt);
        if (BET_KEYS.has(actionKey)) setLastBetSize(facingBet ? amt - lastBetSize : amt);
        else if (actionKey === 'call') setLastBetSize(0);
      }

      if (actionKey === 'fold') {
        setHandOver(true);
        return;
      }

      if (data.deviation?.detected && data.deviation.severity !== 'low') {
        setDeviation(data.deviation);
        await fetchBestResponse(data.weights);
      } else {
        setDeviation(null);
      }
    } catch (err) {
      console.error('Range update failed:', err);
    } finally {
      setRangeLoading(false);
      if (actionKey !== 'fold') {
        const heroIsBet  = capturedHeroAction && BET_NAMES.has(capturedHeroAction);
        const heroChecked = capturedHeroAction === 'Check';
        const streetOver = (actionKey === 'call' && heroIsBet) ||
                           (actionKey === 'check' && heroChecked);
        if (streetOver) {
          setTimeout(() => doAdvanceStreet(), 600);
        } else {
          setHeroAction(null);
          setHeroTurn(true);
        }
      }
    }
  }, [rangeWeights, holeCards, boardCards, oppPosition, potSize, lastBetSize, stackSize, street, heroAction, doAdvanceStreet]);

  const fetchBestResponse = useCallback(async (weights) => {
    setBrLoading(true);
    try {
      const res = await fetch(`${API}/bestresponse`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          hole_cards: holeCards.filter(Boolean),
          board_cards: boardCards.filter(Boolean),
          position,
          pot_size: potSize,
          stack_size: stackSize,
          street,
          range_weights: weights || rangeWeights || Array(1326).fill(1 / 1326),
        }),
      });
      const data = await res.json();
      setBrData(data);
      setActiveTab('exploit');
    } catch (err) {
      console.error('Best response failed:', err);
    } finally {
      setBrLoading(false);
    }
  }, [holeCards, boardCards, position, potSize, stackSize, street, rangeWeights]);


  const handleCardSelect = (card) => {
    if (!pickingSlot) return;
    if (pickingSlot.type === 'hole') {
      const next = [...holeCards];
      next[pickingSlot.idx] = card;
      setHoleCards(next);
    } else {
      const next = [...boardCards];
      next[pickingSlot.idx] = card;
      setBoardCards(next);
    }
    setPickingSlot(null);
  };

  const advanceStreet = () => {
    if (street < 3) {
      setStreet(s => s + 1);
      setActionHistory(prev => [...prev, { divider: true, street: street + 1 }]);
    }
  };


  const boardSlotsForStreet = street === 0 ? 0 : street === 1 ? 3 : street === 2 ? 4 : 5;
  const nextStreetLabel = ['Deal Flop', 'Deal Turn', 'Deal River'][street] || '—';
  const canAdvanceStreet = street < 3 && boardCards.slice(0, boardSlotsForStreet).filter(Boolean).length >= boardSlotsForStreet;
  const heroActsFirst = position < oppPosition;
  const heroFacingBet = (lastVillainAction && BET_KEYS.has(lastVillainAction)) ||
                        (street === 0 && heroActsFirst && !lastVillainAction);
  const villainFacingBet = (heroAction && BET_NAMES.has(heroAction)) ||
                           (street === 0 && !heroActsFirst && !heroAction);

  return (
    <div style={{ minHeight: '100vh', background: '#0d0d1a', color: '#e0e0e0', fontFamily: 'Inter, system-ui, sans-serif' }}>
      <SolveProgress />

      <div style={{ background: '#12121e', borderBottom: '1px solid #2a2a3e', padding: '12px 20px', display: 'flex', alignItems: 'center', gap: 12 }}>
        <span style={{ fontSize: 22, fontWeight: 700, color: '#4a9eff' }}>DEVIATE</span>
        <span style={{ color: '#555', fontSize: 14 }}>GTO Poker Solver & Exploit Finder</span>
        <button
          onClick={resetHand}
          style={{
            marginLeft: 'auto', padding: '6px 14px', fontSize: 12, borderRadius: 6,
            background: '#1e1e2e', border: '1px solid #333', color: '#aaa', cursor: 'pointer',
          }}
        >
          + New Hand
        </button>
      </div>

      <div style={{ display: 'flex', gap: 0, minHeight: 'calc(100vh - 57px)' }}>

        <div style={{ width: 280, background: '#12121e', borderRight: '1px solid #2a2a3e', padding: 16, overflowY: 'auto' }}>
          <h3 style={{ margin: '0 0 12px', color: '#888', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>Your Hand</h3>

          <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
            {holeCards.map((card, i) => (
              <PlayingCard
                key={i} card={card} size="lg"
                selected={pickingSlot?.type === 'hole' && pickingSlot?.idx === i}
                onClick={() => setPickingSlot({ type: 'hole', idx: i })}
              />
            ))}
          </div>

          <h3 style={{ margin: '0 0 8px', color: '#888', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>Board</h3>
          <div style={{ display: 'flex', gap: 6, marginBottom: 4 }}>
            {boardCards.slice(0, 5).map((card, i) => {
              const isActive = i < boardSlotsForStreet;
              return (
                <PlayingCard
                  key={i} card={card} size="md"
                  blocked={!isActive}
                  selected={pickingSlot?.type === 'board' && pickingSlot?.idx === i}
                  onClick={isActive ? () => setPickingSlot({ type: 'board', idx: i }) : undefined}
                />
              );
            })}
          </div>
          {street === 0 && (
            <p style={{ fontSize: 11, color: '#555', margin: '4px 0 12px' }}>Board cards unlock on flop+</p>
          )}

          <div style={{ marginBottom: 12 }}>
            <label style={{ fontSize: 12, color: '#888' }}>Street</label>
            <div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
              {STREETS.map((s, i) => (
                <button
                  key={s}
                  onClick={() => setStreet(i)}
                  style={{
                    flex: 1, padding: '4px 0', fontSize: 11, borderRadius: 4, border: 'none', cursor: 'pointer',
                    background: street === i ? '#4a9eff' : '#1e1e2e', color: street === i ? '#fff' : '#888',
                  }}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>

          <div style={{ marginBottom: 8 }}>
            <label style={{ fontSize: 12, color: '#888' }}>Your Seat</label>
            <div style={{ display: 'flex', gap: 4, marginTop: 4, flexWrap: 'wrap' }}>
              {POSITIONS.map((p, i) => (
                <button
                  key={p}
                  onClick={() => setPosition(i)}
                  style={{
                    padding: '4px 8px', fontSize: 11, borderRadius: 4, border: 'none', cursor: 'pointer',
                    background: position === i ? '#4a9eff' : '#1e1e2e', color: position === i ? '#fff' : '#888',
                  }}
                >
                  {p}
                </button>
              ))}
            </div>
          </div>

          <div style={{ marginBottom: 12 }}>
            <label style={{ fontSize: 12, color: '#888' }}>Opponent Seat</label>
            <div style={{ display: 'flex', gap: 4, marginTop: 4, flexWrap: 'wrap' }}>
              {POSITIONS.map((p, i) => (
                <button
                  key={p}
                  onClick={() => setOppPosition(i)}
                  style={{
                    padding: '4px 8px', fontSize: 11, borderRadius: 4, border: 'none', cursor: 'pointer',
                    background: oppPosition === i ? '#e63946' : '#1e1e2e', color: oppPosition === i ? '#fff' : '#888',
                  }}
                >
                  {p}
                </button>
              ))}
            </div>
            <div style={{
              marginTop: 6, padding: '4px 8px', background: '#1e1e2e', borderRadius: 4,
              fontSize: 11, color: heroActsFirst ? '#4a9eff' : '#f4a261',
              border: `1px solid ${heroActsFirst ? '#2a4a7f' : '#5a3a10'}`,
            }}>
              {heroActsFirst
                ? 'You act first — pick your action in the GTO tab, then log villain\'s response'
                : 'Villain acts first — log their action below, then see your GTO counter'}
            </div>
          </div>

          <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
            <div style={{ flex: 1 }}>
              <label style={{ fontSize: 12, color: '#888' }}>Pot (bb)</label>
              <div style={{
                background: '#1e1e2e', border: '1px solid #2a2a3e', borderRadius: 4,
                padding: '4px 8px', color: '#4a9eff', marginTop: 4, fontSize: 13, fontVariantNumeric: 'tabular-nums',
              }}>
                {potSize.toFixed(1)}
              </div>
            </div>
            <div style={{ flex: 1 }}>
              <label style={{ fontSize: 12, color: '#888' }}>Stack (bb)</label>
              <input
                type="number" value={stackSize} min={1}
                onChange={e => setStackSize(Number(e.target.value))}
                style={{ width: '100%', background: '#1e1e2e', border: '1px solid #333', borderRadius: 4, padding: '4px 8px', color: '#e0e0e0', marginTop: 4, fontSize: 13 }}
              />
            </div>
          </div>

          <button
            onClick={advanceStreet}
            disabled={!canAdvanceStreet}
            title={!canAdvanceStreet && street > 0 ? 'Fill all board cards for this street first' : ''}
            style={{
              width: '100%', padding: '8px 0', background: canAdvanceStreet ? '#2a4a7f' : '#1e1e2e',
              border: 'none', borderRadius: 6, color: canAdvanceStreet ? '#4a9eff' : '#444',
              cursor: canAdvanceStreet ? 'pointer' : 'default', fontSize: 13, marginBottom: 8,
            }}
          >
            → {nextStreetLabel}
          </button>

        </div>

        <div style={{ flex: 1, padding: 20, overflowY: 'auto' }}>

          {deviation?.detected && deviation.severity !== 'low' && (
            <div style={{
              background: deviation.severity === 'high' ? '#2d1010' : '#1e1a08',
              border: `1px solid ${deviation.severity === 'high' ? '#e63946' : '#f4a261'}`,
              borderRadius: 8, padding: '10px 14px', marginBottom: 16,
              display: 'flex', alignItems: 'center', gap: 12,
            }}>
              <span style={{ fontSize: 20 }}>⚠️</span>
              <div>
                <div style={{ color: deviation.severity === 'high' ? '#e63946' : '#f4a261', fontWeight: 600, fontSize: 14 }}>
                  {deviation.severity === 'high' ? 'Major' : 'Significant'} GTO Deviation Detected
                </div>
                <div style={{ color: '#aaa', fontSize: 12 }}>
                  GTO frequency: {(deviation.gto_frequency * 100).toFixed(1)}% — Estimated exploit EV: +{deviation.exploit_ev}bb
                </div>
              </div>
            </div>
          )}

          <div style={{ display: 'flex', gap: 0, marginBottom: 16, borderBottom: '1px solid #2a2a3e' }}>
            {[['gto', 'GTO Strategy'], ['range', 'Opponent Range'], ['exploit', 'Exploit']].map(([key, label]) => (
              <button
                key={key}
                onClick={() => setActiveTab(key)}
                style={{
                  padding: '8px 16px', fontSize: 13, border: 'none', cursor: 'pointer',
                  background: 'none', color: activeTab === key ? '#4a9eff' : '#666',
                  borderBottom: `2px solid ${activeTab === key ? '#4a9eff' : 'transparent'}`,
                  marginBottom: -1,
                }}
              >
                {label}
                {key === 'exploit' && brLoading && ' ⟳'}
              </button>
            ))}
          </div>

          {activeTab === 'gto' && (
            <div>
              {loading && <p style={{ color: '#555' }}>Loading…</p>}
              {!loading && !gtoData && holeCards.filter(Boolean).length < 2 && (
                <p style={{ color: '#555' }}>Select your two hole cards to see GTO strategy.</p>
              )}

              {handOver && (
                <div style={{ padding: '12px 16px', background: '#1a0d0d', border: '1px solid #5a1a1a', borderRadius: 8, marginBottom: 16 }}>
                  <span style={{ color: '#e63946', fontWeight: 600, fontSize: 13 }}>Hand complete (fold). </span>
                  <button onClick={resetHand} style={{ marginLeft: 8, padding: '4px 10px', fontSize: 12, background: '#2a1010', border: '1px solid #5a1a1a', borderRadius: 4, color: '#e63946', cursor: 'pointer' }}>
                    + New Hand
                  </button>
                </div>
              )}

              {!heroTurn && !handOver && (
                <div style={{ marginBottom: 20 }}>
                  {heroAction && (
                    <div style={{ marginBottom: 10, padding: '6px 10px', background: '#0d1a2e', border: '1px solid #2a4a7f', borderRadius: 6, display: 'flex', alignItems: 'center', gap: 10 }}>
                      <span style={{ color: '#4a9eff', fontSize: 12 }}>You chose: <strong>{actionDisplayLabel(heroAction, street)}</strong></span>
                      <button onClick={() => { setHeroAction(null); setHeroTurn(true); }} style={{ fontSize: 11, color: '#555', background: 'none', border: 'none', cursor: 'pointer' }}>undo</button>
                    </div>
                  )}
                  <h4 style={{ margin: '0 0 6px', color: '#f4a261', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>
                    Log Villain's {heroAction ? 'Response' : 'Action'}
                    {rangeLoading && <span style={{ color: '#f4a261', marginLeft: 8, fontWeight: 400 }}>updating range…</span>}
                  </h4>
                  {!heroAction && (
                    <p style={{ margin: '0 0 8px', fontSize: 11, color: '#555' }}>
                      Villain acts first — log their action to update range, then see your GTO response.
                    </p>
                  )}
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    {(() => {
                      const ctx = { street, pot: potSize, lastBet: lastBetSize, stack: stackSize, facingBet: villainFacingBet };
                      return ALL_ACTIONS.filter(a => actionLegal(a.key, ctx)).map(({ key }) => (
                        <button
                          key={key}
                          onClick={() => observeAction(key, villainFacingBet)}
                          disabled={rangeLoading}
                          style={{
                            padding: '6px 12px', fontSize: 12, borderRadius: 6,
                            border: '1px solid #5a3a10',
                            background: rangeLoading ? '#161620' : '#1e1a08',
                            color: rangeLoading ? '#444' : '#f4a261',
                            cursor: rangeLoading ? 'default' : 'pointer',
                          }}
                        >
                          {actionLabel(key, ctx)}
                        </button>
                      ));
                    })()}
                  </div>
                </div>
              )}

              {heroTurn && !handOver && gtoData && (
                <div style={{ maxWidth: 440 }}>
                  <div style={{ color: '#555', fontSize: 12, marginBottom: 8 }}>
                    Bucket #{gtoData.bucket} · {POSITIONS[position]} · {STREETS[street]}
                    {lastVillainAction && (
                      <span style={{ marginLeft: 8, color: '#f4a261' }}>
                        facing {actionDisplayLabel(ALL_ACTIONS.find(a => a.key === lastVillainAction)?.label ?? lastVillainAction, street)}
                        {heroFacingBet && lastBetSize > 0 ? ` · ${fmtBB(lastBetSize)}bb to call` : ''}
                      </span>
                    )}
                  </div>
                  <h4 style={{ margin: '0 0 8px', color: '#4a9eff', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>
                    Your GTO Action
                  </h4>
                  {(() => {
                    const ctx = { street, pot: potSize, lastBet: lastBetSize, stack: stackSize, facingBet: heroFacingBet };
                    return gtoData.strategy
                      ?.filter(s => actionLegal(ACTION_NAME_TO_KEY[s.action] ?? s.action.toLowerCase(), ctx))
                      .map(s => (
                        <StrategyBar key={s.action} action={s.action} label={strategyActionLabel(s.action, ctx)} frequency={s.frequency} ev={s.ev} />
                      ));
                  })()}

                  <div style={{ marginTop: 14 }}>
                    <h4 style={{ margin: '0 0 6px', color: '#4a9eff', fontSize: 11, textTransform: 'uppercase', letterSpacing: 1 }}>
                      Select Your Action
                    </h4>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                      {(() => {
                        const ctx = { street, pot: potSize, lastBet: lastBetSize, stack: stackSize, facingBet: heroFacingBet };
                        return gtoData.strategy
                          ?.filter(s => actionLegal(ACTION_NAME_TO_KEY[s.action] ?? s.action.toLowerCase(), ctx))
                          .map(s => {
                            const actionKey = ACTION_NAME_TO_KEY[s.action] ?? s.action.toLowerCase();
                            const isCall  = s.action === 'Call';
                            const isCheck = s.action === 'Check';
                            const closesStreet = (isCall && BET_KEYS.has(lastVillainAction)) ||
                                                 (isCheck && lastVillainAction === 'check');
                            return (
                              <button
                                key={s.action}
                                onClick={() => {
                                  const amt = actionAmount(actionKey, ctx);
                                  if (amt > 0) {
                                    setPotSize(prev => prev + amt);
                                    if (BET_KEYS.has(actionKey)) setLastBetSize(heroFacingBet ? amt - lastBetSize : amt);
                                    else if (isCall) setLastBetSize(0);
                                  }
                                  if (s.action === 'Fold') { setHeroAction(s.action); setHandOver(true); return; }
                                  setHeroAction(s.action);
                                  if (closesStreet) {
                                    setTimeout(() => doAdvanceStreet(), 600);
                                  } else {
                                    setHeroTurn(false);
                                  }
                                }}
                                style={{
                                  padding: '6px 10px', fontSize: 12, borderRadius: 6,
                                  border: '1px solid #2a4a7f', background: '#0d1a2e',
                                  color: '#4a9eff', cursor: 'pointer',
                                }}
                              >
                                {actionLabel(actionKey, ctx)} <span style={{ color: '#446', fontSize: 10 }}>{Math.round(s.frequency * 100)}%</span>
                              </button>
                            );
                          });
                      })()}
                    </div>
                  </div>
                </div>
              )}

              {actionHistory.length > 0 && (
                <div style={{ marginTop: 20 }}>
                  <h4 style={{ margin: '0 0 6px', color: '#555', fontSize: 11, textTransform: 'uppercase' }}>History</h4>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    {actionHistory.map((item, i) =>
                      item.divider
                        ? <span key={i} style={{ color: '#444', fontSize: 11 }}>│ {STREETS[item.street]} │</span>
                        : <span key={i} style={{ background: '#1e1e2e', padding: '2px 8px', borderRadius: 4, fontSize: 11, color: '#888' }}>
                            {actionDisplayLabel(ALL_ACTIONS.find(a => a.key === item.action)?.label ?? item.action, item.street)}
                          </span>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}

          {activeTab === 'range' && (
            <div>
              <h4 style={{ margin: '0 0 8px', color: '#888', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>
                Opponent Range Posterior
              </h4>
              <p style={{ color: '#555', fontSize: 12, marginBottom: 10 }}>
                Click what villain did to update their estimated range.
                {rangeLoading && <span style={{ color: '#f4a261', marginLeft: 6 }}>updating…</span>}
              </p>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 16 }}>
                {ALL_ACTIONS.map(({ key, label }) => (
                  <button
                    key={key}
                    onClick={() => observeAction(key)}
                    disabled={rangeLoading}
                    style={{
                      padding: '6px 12px', fontSize: 12, borderRadius: 6,
                      border: '1px solid #333',
                      background: rangeLoading ? '#161620' : '#1e1e2e',
                      color: rangeLoading ? '#444' : '#ccc',
                      cursor: rangeLoading ? 'default' : 'pointer',
                    }}
                  >
                    {actionDisplayLabel(label, street)}
                  </button>
                ))}
              </div>
              {rangeWeights ? (
                <>
                  {rangeStats && (
                    <div style={{ color: '#888', fontSize: 12, marginBottom: 12 }}>
                      Active combos: {rangeStats.num_active}
                    </div>
                  )}
                  <RangeHeatmap rangeWeights={rangeWeights} />
                </>
              ) : (
                <p style={{ color: '#444', fontSize: 12 }}>No range data yet — observe an action above.</p>
              )}
            </div>
          )}

          {activeTab === 'exploit' && (
            <div>
              {brLoading && <p style={{ color: '#555' }}>Computing best response…</p>}
              {!brLoading && (
                <button
                  onClick={() => fetchBestResponse(rangeWeights)}
                  disabled={holeCards.filter(Boolean).length < 2}
                  style={{
                    marginBottom: 14, padding: '8px 16px',
                    background: holeCards.filter(Boolean).length < 2 ? '#1e1e2e' : '#1e2a1e',
                    border: '1px solid #2d5a2d', borderRadius: 6,
                    color: holeCards.filter(Boolean).length < 2 ? '#444' : '#4caf50',
                    cursor: holeCards.filter(Boolean).length < 2 ? 'default' : 'pointer', fontSize: 13,
                  }}
                >
                  {brData ? 'Recompute Exploit' : 'Compute Exploit'}
                </button>
              )}
              {!brLoading && !brData && (
                <p style={{ color: '#555', fontSize: 12 }}>
                  Select hole cards, optionally observe opponent actions on the Range tab, then click Compute Exploit.
                </p>
              )}
              {brData && (() => {
                const ctx = { street, pot: potSize, lastBet: lastBetSize, stack: stackSize, facingBet: heroFacingBet };
                const keyOf = (name) => ACTION_NAME_TO_KEY[name] ?? name.toLowerCase();
                const legal = (brData.actions || []).filter(a => actionLegal(keyOf(a.action), ctx));
                if (legal.length === 0) {
                  return <p style={{ color: '#555', fontSize: 12 }}>No actions available in this spot.</p>;
                }
                const best = legal.reduce((m, a) => (a.ev > m.ev ? a : m), legal[0]);
                const freqByName = Object.fromEntries((gtoData?.strategy || []).map(s => [s.action, s.frequency]));
                let fSum = 0, gtoEV = 0;
                legal.forEach(a => { const f = freqByName[a.action] || 0; fSum += f; gtoEV += f * a.ev; });
                const gain = fSum > 0 ? Math.round((best.ev - gtoEV / fSum) * 100) / 100 : null;

                const sorted = [...legal].sort((a, b) => b.ev - a.ev);
                const evs = sorted.map(a => a.ev);
                const maxEv = Math.max(...evs, 0), minEv = Math.min(...evs, 0), span = (maxEv - minEv) || 1;
                return (
                  <div style={{ maxWidth: 420 }}>
                    <div style={{ marginBottom: 14, padding: '10px 14px', background: '#0d1e0d', border: '1px solid #2d5a2d', borderRadius: 8 }}>
                      <div style={{ color: '#4caf50', fontWeight: 600, fontSize: 14 }}>Best Exploit: {actionLabel(keyOf(best.action), ctx)}</div>
                      {gain !== null && (
                        <div style={{ color: '#aaa', fontSize: 12, marginTop: 4 }}>
                          EV gain vs GTO: {gain >= 0 ? '+' : ''}{gain}bb
                        </div>
                      )}
                    </div>
                    {sorted.map(a => {
                      const isBest = a.action === best.action;
                      const w = Math.round(((a.ev - minEv) / span) * 100);
                      return (
                        <div key={a.action} style={{ marginBottom: 6 }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 2 }}>
                            <span style={{ color: isBest ? '#4caf50' : '#aaa', fontWeight: isBest ? 600 : 400 }}>
                              {actionLabel(keyOf(a.action), ctx)}{isBest ? ' ◀ best' : ''}
                            </span>
                            <span style={{ color: '#aaa' }}>{a.ev >= 0 ? '+' : ''}{a.ev}bb</span>
                          </div>
                          <div style={{ height: 8, background: '#1e1e2e', borderRadius: 4, overflow: 'hidden' }}>
                            <div style={{ width: `${w}%`, height: '100%', background: isBest ? '#4caf50' : '#2d5a2d', borderRadius: 4, transition: 'width 0.3s' }} />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                );
              })()}
            </div>
          )}
        </div>
      </div>

      {pickingSlot && (
        <div
          style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.7)', zIndex: 100, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          onClick={() => setPickingSlot(null)}
        >
          <div style={{ background: '#12121e', padding: 16, borderRadius: 12, border: '1px solid #333' }} onClick={e => e.stopPropagation()}>
            <div style={{ fontSize: 12, color: '#888', marginBottom: 8 }}>
              Pick card for {pickingSlot.type === 'hole' ? 'hole' : 'board'} slot {pickingSlot.idx + 1}
            </div>
            <CardPicker usedCards={usedCards} onSelect={handleCardSelect} />
            <button onClick={() => setPickingSlot(null)} style={{ marginTop: 8, fontSize: 12, color: '#555', background: 'none', border: 'none', cursor: 'pointer' }}>
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
