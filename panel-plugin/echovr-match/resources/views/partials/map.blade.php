@php
    use MarshmallowMia\EchoVrMatch\Services\Format;
    // Top-down view of the players' heads and the disc on the floor plane, in
    // the game's meters: z (goal to goal) runs left to right, x top to bottom.
    // A level with a drawing (Format::mapDrawing) shows it at its true size,
    // and its dots follow the server's live.json several times a second
    // (echovrLiveMap); otherwise the box is what the match has seen so far, at
    // least 12 m wide, and moves with the page's refresh.
    $dots = [];
    foreach (['blue', 'orange'] as $side) {
        foreach ($m[$side]['players'] ?? [] as $p) {
            if (count($p['pos'] ?? []) === 3) {
                $dots[] = ['x' => $p['pos'][0], 'z' => $p['pos'][2], 'side' => $side, 'name' => $p['name'], 'p' => $p];
            }
        }
    }
    $disc = count($m['disc']['pos'] ?? []) === 3 ? $m['disc']['pos'] : null;
    $drawing = Format::mapDrawing($m['map'] ?? null);
    $b = $m['bounds'] ?? null;
    // In a lobby the game refuses /session: the supervisor writes live.json from
    // nEVR's telemetry instead, and the map shows while it does ($live['now']).
    $lobby = Format::isLobby($m);
    $liveUrl = $drawing && !empty($live['url']) && ($live['ms'] ?? 0) > 0 && ($live['now'] ?? true) ? $live['url'] : null;
@endphp
@if ((count($dots) > 0 || $liveUrl) && ($drawing || $b))
    @php
        if ($drawing) {
            [$minZ, $minX, $w, $h] = $drawing['view'];
            $r = 0.8; // about a player's size
            $label = '#e6e6ee';
        } else {
            $pad = 2;
            [$minX, $maxX, $minZ, $maxZ] = [$b['min_x'] - $pad, $b['max_x'] + $pad, $b['min_z'] - $pad, $b['max_z'] + $pad];
            if ($maxX - $minX < 12) { $c = ($minX + $maxX) / 2; $minX = $c - 6; $maxX = $c + 6; }
            if ($maxZ - $minZ < 12) { $c = ($minZ + $maxZ) / 2; $minZ = $c - 6; $maxZ = $c + 6; }
            [$w, $h] = [$maxZ - $minZ, $maxX - $minX];
            $r = max($w, $h) / 70;
            $label = 'currentColor';
        }
        $at = fn ($x, $z) => [round($z, 2), round($x, 2)];
        $view = implode(' ', array_map(fn ($v) => round($v, 2), [$minZ, $minX, $w, $h]));
        // A label sits above its dot (below it at the top edge) and stays inside the map; the
        // live script places it the same way.
        $labelAt = fn ($cx, $cy) => [
            round(min(max($cx, $minZ + $r * 5), $minZ + $w - $r * 5) - $cx, 2),
            round($cy - $r * 2.4 < $minX + $r * 2 ? $r * 3.8 : -$r * 2.4, 2),
        ];
        $font = round($r * ($drawing ? 1.9 : 1.3), 2);
        $cfg = [
            'url' => $liveUrl, 'ms' => (int) ($live['ms'] ?? 0), 'r' => $r, 'label' => $label, 'font' => $font,
            'view' => [$minZ, $minX, $w, $h],
            'colors' => ['blue' => Format::teamColor('blue'), 'orange' => Format::teamColor('orange'), 'social' => Format::teamColor('social')],
        ];
    @endphp
    <x-filament::section :heading="trans('echovr-match::match.map')" collapsible>
        <svg viewBox="{{ $view }}" preserveAspectRatio="xMidYMid meet" role="img" aria-label="{{ trans('echovr-match::match.map') }}"
            style="width:100%;{{ $drawing ? 'aspect-ratio:' . $w . '/' . $h . ';max-height:80vh;background:#16161a' : 'max-height:22rem;background:rgb(127 127 127 / .08)' }};border-radius:.5rem;display:block">
            @if ($drawing)
                <image href="{{ $drawing['url'] }}" x="{{ $minZ }}" y="{{ $minX }}" width="{{ $w }}" height="{{ $h }}" preserveAspectRatio="none" />
            @endif
            {{-- With live positions the script owns this layer and the page's refresh leaves it alone. --}}
            <g @if ($liveUrl) wire:ignore wire:key="echovr-live-{{ $m['session_id'] ?? '' }}" x-data="echovrLiveMap(@js($cfg))" @endif>
                @foreach ($dots as $d)
                    @php [$cx, $cy] = $at($d['x'], $d['z']); [$lx, $ly] = $labelAt($cx, $cy); @endphp
                    <g data-player="{{ $d['name'] }}" opacity="{{ !empty($d['p']['stunned']) ? '.45' : '1' }}" style="transform:translate({{ $cx }}px,{{ $cy }}px)">
                        <title>{{ $d['name'] }}</title>
                        <circle cx="0" cy="0" r="{{ round($r * 1.4, 2) }}" fill="{{ Format::teamColor($d['side']) }}"
                            stroke="{{ $label }}" stroke-width="{{ !empty($d['p']['possession']) ? round($r / 2, 2) : 0 }}" />
                        <text x="{{ $lx }}" y="{{ $ly }}" font-size="{{ $font }}" text-anchor="middle" fill="{{ $label }}"
                            @if ($drawing) stroke="#16161a" stroke-width="{{ round($r * 0.5, 2) }}" paint-order="stroke" stroke-linejoin="round" @else opacity=".85" @endif>{{ \Illuminate\Support\Str::limit($d['name'], 12) }}</text>
                    </g>
                @endforeach
                @if ($disc || $liveUrl)
                    @php [$cx, $cy] = $disc ? $at($disc[0], $disc[2]) : [0, 0]; @endphp
                    <g data-disc style="transform:translate({{ $cx }}px,{{ $cy }}px)" @if (!$disc) display="none" @endif>
                        <title>{{ trans('echovr-match::match.disc') }}</title>
                        <circle cx="0" cy="0" r="{{ round($r * 0.8, 2) }}" fill="none" stroke="{{ $label }}" stroke-width="{{ round($r / 2.5, 2) }}" />
                    </g>
                @endif
            </g>
        </svg>
        <p style="margin-top:.5rem;font-size:.75rem;opacity:.6">
            {{ trans($lobby && $drawing ? 'echovr-match::match.map_note_lobby' : ($drawing ? 'echovr-match::match.map_note_drawn' : 'echovr-match::match.map_note')) }}
            @if (isset($m['disc']['bounces'])) · {{ trans('echovr-match::match.bounces', ['n' => $m['disc']['bounces']]) }} @endif
        </p>
    </x-filament::section>
@endif
