import { describe, it, expect, vi } from 'vitest'
import { AttachmentDrafts, type UploadedAttachment } from '../src/shared/workspace/attachment-drafts'
const receipt = (id: string): UploadedAttachment => ({id,name:id,path:`uploads/${id}`,mime:'text/plain',size:1,kind:'file'})
const tick = async () => { for(let i=0;i<8;i++) await Promise.resolve() }
describe('attachment draft ownership', () => {
  it('keeps uploads in their originating session and bounds concurrency', async () => {
    const pending: ((result: UploadedAttachment) => void)[] = []
    const upload = vi.fn(() => new Promise<UploadedAttachment>(resolve => pending.push(resolve)))
    const drafts = new AttachmentDrafts(upload)
    drafts.add('a',[new File(['a'],'a.txt'),new File(['b'],'b.txt'),new File(['c'],'c.txt')])
    expect(upload).toHaveBeenCalledTimes(2)
    drafts.add('b',[new File(['d'],'d.txt')])
    pending[0]!(receipt('a')); await tick()
    expect(upload).toHaveBeenCalledTimes(3)
    expect(drafts.list('a')[0]?.status).toBe('ready')
    expect(drafts.list('b')[0]?.status).toBe('uploading')
    let closed = false
    drafts.subscribe('a',() => { closed = drafts.closed })
    drafts.dispose()
    expect(closed).toBe(true)
    pending[1]!(receipt('b')); await tick()
    expect(drafts.list('a')).toEqual([])
  })
  it('removal prevents stale upload completion and revokes the preview; retries retain identity', async () => {
    let finish!: (value: UploadedAttachment) => void
    const upload = vi.fn().mockRejectedValueOnce(new Error('offline')).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const drafts = new AttachmentDrafts(upload)
    const revoke = vi.spyOn(URL,'revokeObjectURL')
    drafts.add('a',[new File(['a'],'a.png',{type:'image/png'})]); await tick()
    const item = drafts.list('a')[0]!
    expect(item.status).toBe('error')
    drafts.retry('a',item.id); expect(drafts.list('a')[0]?.id).toBe(item.id)
    drafts.remove('a',item.id); finish(receipt('a')); await tick()
    expect(drafts.list('a')).toEqual([]); expect(revoke).toHaveBeenCalledWith(item.previewUrl)
    drafts.dispose(); revoke.mockRestore()
  })
})
