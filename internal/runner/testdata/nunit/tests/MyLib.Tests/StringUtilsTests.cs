using NUnit.Framework;

namespace MyLib.Tests;

public class StringUtilsTests
{
    [Test]
    public void ReverseString()
    {
        Assert.That("ab".Length, Is.EqualTo(2));
    }
}
