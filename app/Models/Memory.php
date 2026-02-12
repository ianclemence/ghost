<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

class Memory extends Model
{
    protected $fillable = [
        'user_id',
        'content',
        'embedding',
        'importance',
    ];

    /**
     * Get the attributes that should be cast.
     *
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'embedding' => 'array',
        ];
    }

    /**
     * Get the user that owns the memory.
     */
    public function user(): BelongsTo
    {
        return $this->belongsTo(User::class);
    }
}
